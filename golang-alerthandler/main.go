package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/gomail.v2"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type AlertMessage struct {
	Alerts []Alert `json:"alerts"`
}

var (
	grafanaURL     string
	grafanaAPIKey  string
	smtpServer     string
	smtpPort       int
	senderEmail    string
	senderPassword string
	recipientEmail string
)

func init() {
	log.Println("Initializing alert handler...")

	grafanaURL = os.Getenv("GRAFANA_URL")
	grafanaAPIKey = os.Getenv("GRAFANA_API_KEY")
	smtpServer = os.Getenv("SMTP_SERVER")
	senderEmail = os.Getenv("SENDER_EMAIL")
	senderPassword = os.Getenv("SENDER_PASSWORD")
	recipientEmail = os.Getenv("RECIPIENT_EMAIL")
	smtpPort = getEnvAsInt("SMTP_PORT", 587)

	log.Printf("configuration: GRAFANA_URL=%s, SMTP_SERVER=%s, SENDER_EMAIL=%s, RECIPIENT_EMAIL=%s, SMTP_PORT=%d",
		grafanaURL, smtpServer, senderEmail, recipientEmail, smtpPort)

	if grafanaURL == "" || grafanaAPIKey == "" || smtpServer == "" || senderEmail == "" || senderPassword == "" || recipientEmail == "" {
		log.Fatal("missing required environment variables")
	}
}

func getEnvAsInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
		log.Printf("error converting %s to int, using fallback value\n", key)
	}
	return fallback
}

func createGrafanaSnapshot(dashboardUID string) (string, error) {
	// get the complete dashboard configuration
	dashboardURL := fmt.Sprintf("%s/api/dashboards/uid/%s", grafanaURL, dashboardUID)
	req, err := http.NewRequest("GET", dashboardURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating dashboard request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+grafanaAPIKey)

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending dashboard request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var dashboardResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&dashboardResp); err != nil {
		return "", fmt.Errorf("error decoding dashboard response: %w", err)
	}

	dashboard, ok := dashboardResp["dashboard"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("dashboard data not found in response")
	}

	// create the snapshot
	snapshotURL := fmt.Sprintf("%s/api/snapshots", grafanaURL)
	payload := map[string]interface{}{
		"dashboard": dashboard,
		"expires":   3600, // expires in 1 hour
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshalling snapshot payload: %w", err)
	}

	req, err = http.NewRequest("POST", snapshotURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("error creating snapshot request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+grafanaAPIKey)

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending snapshot request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error decoding snapshot response: %w", err)
	}

	snapshotURL, ok = result["url"].(string)
	if !ok {
		return "", fmt.Errorf("snapshot URL not found in response")
	}

	return snapshotURL, nil
}

func sendEmailWithSnapshotLink(subject, body, snapshotURL string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", senderEmail)
	m.SetHeader("To", recipientEmail)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", fmt.Sprintf("%s<br><br>Grafana Snapshot: <a href='%s'>View Snapshot</a>", body, snapshotURL))

	d := gomail.NewDialer(smtpServer, smtpPort, senderEmail, senderPassword)
	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func handleAlert(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received %s request to %s", r.Method, r.URL.Path)

	log.Println("request Headers:")
	for name, values := range r.Header {
		for _, value := range values {
			log.Printf("%s: %s", name, value)
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("error reading request body: %v", err)
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
	log.Printf("request body:\n%s", string(body))

	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var alertMessage AlertMessage
	if err := json.NewDecoder(r.Body).Decode(&alertMessage); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, alert := range alertMessage.Alerts {
		log.Printf("received alert: %s\n", alert.Annotations["summary"])

		dashboardUID := os.Getenv("DASHBOARD_UID")

		snapshotURL, err := createGrafanaSnapshot(dashboardUID)
		if err != nil {
			log.Printf("error creating Grafana snapshot: %v\n", err)
			snapshotURL = "failed to get snapshot url"
		}

		subject := fmt.Sprintf("Monitor Alert: %s", alert.Annotations["summary"])
		body := fmt.Sprintf("<h1>%s</h1><p>%s</p>", alert.Annotations["summary"], alert.Annotations["description"])

		if err := sendEmailWithSnapshotLink(subject, body, snapshotURL); err != nil {
			log.Printf("error sending email: %v\n", err)
			http.Error(w, "error sending email", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	log.Println("alert processing completed")
}

func main() {
	port := os.Getenv("PORT")
	http.HandleFunc("/alert", handleAlert)
	log.Printf("starting server on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
