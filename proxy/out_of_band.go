package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/proxy/interactsh"
	"github.com/projectdiscovery/interactsh/pkg/server"
)

var oob_client *interactsh.Client = nil
var generation_chan chan bool = nil

func generateOOBClient() error {
	var err error
	generation_chan = make(chan bool)
	defer close(generation_chan)
	fmt.Printf("Generating new interactsh client, this may take a while...\n")
	oob_client, err = interactsh.New(&interactsh.Options{
		ServerURL:         "https://interact.sh",
		PersistentSession: true,
	})

	if err != nil {
		return err
	}

	client_json, err := oob_client.ToJSON()
	if err != nil {
		return err
	}

	project.SetSetting("oob_client", client_json)
	fmt.Printf("Interactsh client generated\n")
	return nil
}

func getOOBClient() (*interactsh.Client, error) {
	if generation_chan != nil {
		// wait until generation has finished
		<-generation_chan
	}
	if oob_client != nil {
		return oob_client, nil
	}

	var err error
	client_json := project.GetSetting("oob_client")

	if client_json == "" {
		err := generateOOBClient()
		if err != nil {
			return oob_client, err
		}
	} else {
		oob_client, err = interactsh.ClientFromJSON(client_json)
		if err != nil {
			fmt.Printf("Could not create or retrieve interactsh client, generating new client: %s\n", err.Error())

			err = generateOOBClient()
			if err != nil {
				close(generation_chan)
				return oob_client, err
			}
		}
	}

	oobStartPolling()

	return oob_client, nil
}

func oobStartPolling() {
	oob_client.StartPolling(time.Duration(5)*time.Second, func(interaction *server.Interaction) {
		url := interaction.Protocol + "://" + interaction.FullId + ".interact.sh"
		verb := ""

		if interaction.Protocol == "http" {
			interaction.Protocol = "http(s)"
			requestReader := bufio.NewReader(strings.NewReader(interaction.RawRequest))
			request, err := http.ReadRequest(requestReader)

			if err == nil {
				request.URL.Scheme = interaction.Protocol
				request.URL.Host = interaction.FullId + ".interact.sh"
				url = request.URL.String()
				verb = request.Method
			}
		}

		addr := interaction.RemoteAddress
		lookup, _ := net.LookupAddr(addr)
		if len(lookup) != 0 {
			addr += " (" + strings.Join(lookup, ", ") + ")"
		}

		displayProperties := map[string]interface{}{
			"Protocol":       strings.ToUpper(interaction.Protocol),
			"Remote Address": addr,
			"Query Type":     interaction.QType,
			"SMTP From":      interaction.SMTPFrom,
		}

		displayPropertiesJson, _ := json.Marshal(displayProperties)

		request := project.Request{
			URL:          url,
			Time:         interaction.Timestamp.Unix(),
			Protocol:     "Out of Band",
			ResponseSize: int64(len(interaction.RawRequest)),
			Verb:         verb,
		}

		requestDataPacket := project.DataPacket{
			Time:        request.Time,
			Data:        []byte(interaction.RawRequest),
			Direction:   "Request",
			DisplayData: string(displayPropertiesJson),
		}

		responseDataPacket := project.DataPacket{
			Time:      request.Time,
			Data:      []byte(interaction.RawResponse),
			Direction: "Response",
		}

		request.DataPackets = append(request.DataPackets, requestDataPacket)
		request.DataPackets = append(request.DataPackets, responseDataPacket)

		request.Record()

	})
}

func StartOutOfBandClient() error {
	_, err := getOOBClient()
	return err
}

func CloseOutOfBandClient() {
	if oob_client != nil {
		oob_client.StopPolling()
		oob_client.Close()
	}
}
