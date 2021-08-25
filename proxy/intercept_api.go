package proxy

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
)

type InterceptSettings struct {
	BrowserToServer bool
	ServerToBrowser bool
}

type InterceptedRequestResponse struct {
	GUID          string
	Body          string `example:"<base64 encoded body>"`
	Direction     string `example:"Either browser_to_server or server_to_browser"`
	RequestAction string `example:"One of: forward, forward_and_intercept_response or drop"`
}

var interceptedRequests []*project.InterceptedRequest
var interceptedRequestsLock sync.Mutex

func interceptRequest(request *project.Request, direction string, requestData []byte) *project.InterceptedRequest {
	interceptedRequest := &project.InterceptedRequest{
		Request:       request,
		Body:          base64.StdEncoding.EncodeToString(requestData),
		Direction:     direction,
		ResponseReady: make(chan bool),
	}

	interceptedRequest.Record(project.RecordActionAdd)

	interceptedRequestsLock.Lock()
	interceptedRequests = append(interceptedRequests, interceptedRequest)
	interceptedRequestsLock.Unlock()

	return interceptedRequest
}

func removeInterceptedRequest(request *project.InterceptedRequest) {
	interceptedRequestsLock.Lock()

	var idx = -1
	for i, req := range interceptedRequests {
		if req.Request.GUID == request.Request.GUID {
			idx = i
			break
		}
	}

	if idx != -1 {
		interceptedRequests[idx] = interceptedRequests[len(interceptedRequests)-1]
		interceptedRequests = interceptedRequests[:len(interceptedRequests)-1]
	}

	interceptedRequestsLock.Unlock()
}

// GetInterceptRequests godoc
// @Summary Get Intercept Requests
// @Description gets a list of all requests which have been intercepted, which are awaiting a response
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} proxy.InterceptedRequest
// @Failure 500 {string} string Error
// @Router /proxy/intercepted_requests [get]
func GetInterceptedRequests(w http.ResponseWriter, r *http.Request) {
	interceptedRequestsLock.Lock()
	response, err := json.Marshal(interceptedRequests)
	if interceptedRequests == nil {
		response = []byte("[]")
	}
	interceptedRequestsLock.Unlock()

	if err != nil {
		http.Error(w, "Could not marshal intercepted requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// InterceptSettings godoc
// @Summary Get Intercept Settings
// @Description get intercept settings
// @Tags Settings
// @Security ApiKeyAuth
// @Success 200 {object} proxy.InterceptSettings
// @Failure 500 {string} string Error
// @Router /proxy/intercept_settings [get]
func getInterceptSettings(w http.ResponseWriter, r *http.Request) {
	js, err := json.Marshal(interceptSettings)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// InterceptSettings godoc
// @Summary Set Intercept Settings
// @Description set intercept settings
// @Tags Settings
// @Security ApiKeyAuth
// @Param default body proxy.InterceptSettings true "Proxy Intercept Settings Object"
// @Success 200
// @Failure 500 {string} string Error
// @Router /proxy/intercept_settings [put]
func setInterceptSettings(w http.ResponseWriter, r *http.Request) {
	err := json.NewDecoder(r.Body).Decode(&interceptSettings)
	if err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// release any requests in the queue
	interceptedRequestsLock.Lock()
	for _, req := range interceptedRequests {
		if !interceptSettings.BrowserToServer && req.Direction == "browser_to_server" {
			req.ResponseReady <- true
			req.Record(project.RecordActionDelete)
		}

		if !interceptSettings.ServerToBrowser && req.Direction == "server_to_browser" {
			req.ResponseReady <- true
			req.Record(project.RecordActionDelete)
		}
	}

	interceptedRequestsLock.Unlock()
}

// InterceptSettings godoc
// @Summary Modify Intercepted Request
// @Description set how an intercepted request will be responded to
// @Tags Settings
// @Security ApiKeyAuth
// @Param default body proxy.InterceptedRequestResponse true "Proxy Intercept Response Object"
// @Success 200
// @Failure 500 {string} string Error
// @Router /proxy/set_intercepted_response [put]
func SetInterceptedResponse(w http.ResponseWriter, r *http.Request) {
	var response InterceptedRequestResponse
	err := json.NewDecoder(r.Body).Decode(&response)
	if err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	interceptedRequestsLock.Lock()
	for _, req := range interceptedRequests {
		if req.Direction == response.Direction && req.Request.GUID == response.GUID {
			requestBytes, _ := base64.StdEncoding.DecodeString(response.Body)
			direction := "Request"
			if response.Direction == "server_to_browser" {
				direction = "Response"
			}
			req.RequestAction = response.RequestAction
			req.Request.DataPackets = append(req.Request.DataPackets, project.DataPacket{Data: requestBytes, Direction: direction, Modified: true})
			req.ResponseReady <- true
			req.Record(project.RecordActionDelete)
		}
	}

	interceptedRequestsLock.Unlock()
}

func HandleInterceptSettingsRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getInterceptSettings(w, r)
	case http.MethodPut:
		setInterceptSettings(w, r)
	default:
		http.Error(w, "Unsupported method", http.StatusInternalServerError)
	}
}