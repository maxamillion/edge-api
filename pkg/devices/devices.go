package devices

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/redhatinsights/edge-api/config"
	"github.com/redhatinsights/edge-api/pkg/common"
	log "github.com/sirupsen/logrus"
)

// Inventory list of devices
type Inventory struct {
	Total  int       `json:"total"`
	Count  int       `json:"count"`
	Result []devices `json:"results"`
}

type devices struct {
	ID     string        `json:"id"`
	Ostree systemProfile `json:"system_profile"`
}

type systemProfile struct {
	RpmOstreeDeployments []ostree `json:"rpm_ostree_deployments"`
}

type ostree struct {
	Checksum string `json:"checksum"`
	Booted   bool   `json:"booted"`
}

const (
	inventoryAPI = "api/inventory/v1/hosts"
	orderBy      = "updated"
	orderHow     = "DESC"
	filterParams = "?filter[system_profile][host_type]=edge&fields[system_profile]=host_type,operating_system,greenboot_status,greenboot_fallback_detected,rpm_ostree_deployments"
)

// ReturnDevices will return the list of devices without filter by tag or uuid
func ReturnDevices(w http.ResponseWriter, r *http.Request) (Inventory, error) {
	url := fmt.Sprintf("%s/api/inventory/v1/hosts", config.Get().InventoryConfig.URL)
	fullURL := url + filterParams
	log.Infof("Requesting url: %s\n", fullURL)
	req, _ := http.NewRequest("GET", fullURL, nil)
	req.Header.Add("Content-Type", "application/json")
	headers := common.GetOutgoingHeaders(r)
	for key, value := range headers {
		req.Header.Add(key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error(fmt.Printf("ReturnDevices: %s", err))
		return Inventory{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(fmt.Printf("ReturnDevices: %s", err))
		return Inventory{}, err
	}
	defer resp.Body.Close()
	var bodyResp Inventory
	json.Unmarshal([]byte(body), &bodyResp)
	log.Infof("struct: %v\n", bodyResp)
	return bodyResp, nil

}

// ReturnDevicesByID will return the list of devices by uuid
func ReturnDevicesByID(w http.ResponseWriter, r *http.Request) (Inventory, error) {
	deviceID := r.URL.Query().Get("DeviceUUID")
	deviceIDParam := "&hostname_or_id=" + deviceID

	url := fmt.Sprintf("%s/api/inventory/v1/hosts", config.Get().InventoryConfig.URL)
	fullURL := url + filterParams + deviceIDParam
	log.Infof("Requesting url: %s\n", fullURL)
	req, _ := http.NewRequest("GET", fullURL, nil)
	req.Header.Add("Content-Type", "application/json")
	headers := common.GetOutgoingHeaders(r)
	for key, value := range headers {
		req.Header.Add(key, value)
	}
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Error(fmt.Printf("ReturnDevicesByID: %s", err))
		return Inventory{}, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Error(fmt.Printf("error requesting inventory, got status code %d and body %s", resp.StatusCode, body))
		return Inventory{}, fmt.Errorf("error requesting inventory, got status code %d and body %s", resp.StatusCode, body)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(fmt.Printf("ReturnDevicesByID: %s", err))
		return Inventory{}, err
	}
	log.Infof("fullbody: %v\n", string(body))
	defer resp.Body.Close()
	var bodyResp Inventory
	json.Unmarshal([]byte(body), &bodyResp)
	log.Infof("struct: %v\n", bodyResp)

	return bodyResp, nil

}

// ReturnDevicesByTag will return the list of devices by tag
func ReturnDevicesByTag(w http.ResponseWriter, r *http.Request) (Inventory, error) {

	tags := r.URL.Query().Get("tag")
	tagsParam := "?tags=" + tags

	url := fmt.Sprintf("%s/api/inventory/v1/hosts", config.Get().InventoryConfig.URL)
	fullURL := url + filterParams + tagsParam
	log.Infof("Requesting url: %s\n", fullURL)
	req, _ := http.NewRequest("GET", fullURL, nil)
	req.Header.Add("Content-Type", "application/json")
	headers := common.GetOutgoingHeaders(r)
	for key, value := range headers {
		req.Header.Add(key, value)
	}
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Error(fmt.Printf("ReturnDevicesByTag: %s", err))
		return Inventory{}, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Error(fmt.Printf("error requesting inventory, got status code %d and body %s", resp.StatusCode, body))
		return Inventory{}, fmt.Errorf("error requesting inventory, got status code %d and body %s", resp.StatusCode, body)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(fmt.Printf("ReturnDevicesByTag: %s", err))
		return Inventory{}, err
	}
	var bodyResp Inventory
	json.Unmarshal([]byte(body), &bodyResp)
	log.Infof("struct: %v\n", bodyResp)
	return bodyResp, nil
}
