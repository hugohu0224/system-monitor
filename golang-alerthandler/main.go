package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"gopkg.in/gomail.v2"
)

type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type AlertMessage struct {
	Alerts []Alert `json:"alerts"`
}

type Config struct {
	GrafanaURL     string
	GrafanaAPIKey  string
	SMTPServer     string
	SMTPPort       int
	SenderEmail    string
	SenderPassword string
	RecipientEmail string
	DashboardUID   string
	Port           string
}

var cfg Config

func init() {
	log.Println("Initializing alert handler...")
	cfg = loadConfig()
	log.Printf("Configuration: GRAFANA_URL=%s, SMTP_SERVER=%s, SENDER_EMAIL=%s, RECIPIENT_EMAIL=%s, SMTP_PORT=%d",
		cfg.GrafanaURL, cfg.SMTPServer, cfg.SenderEmail, cfg.RecipientEmail, cfg.SMTPPort)
}

func loadConfig() Config {
	return Config{
		GrafanaURL:     getEnvOrFatal("GRAFANA_URL"),
		GrafanaAPIKey:  getEnvOrFatal("GRAFANA_API_KEY"),
		SMTPServer:     getEnvOrFatal("SMTP_SERVER"),
		SenderEmail:    getEnvOrFatal("SENDER_EMAIL"),
		SenderPassword: getEnvOrFatal("SENDER_PASSWORD"),
		RecipientEmail: getEnvOrFatal("RECIPIENT_EMAIL"),
		SMTPPort:       getEnvAsInt("SMTP_PORT", 587),
		DashboardUID:   getEnvOrFatal("DASHBOARD_UID"),
		Port:           getEnvOrFatal("PORT"),
	}
}

func getEnvOrFatal(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("Missing required environment variable: %s", key)
	}
	return value
}

func getEnvAsInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
		log.Printf("error converting %s to int, using fallback value", key)
	}
	return fallback
}

func createGrafanaSnapshot(dashboardUID string) (string, error) {
	dashboard, err := getDashboardConfig(dashboardUID)
	if err != nil {
		return "", fmt.Errorf("error getting dashboard config: %w", err)
	}

	snapshotURL, err := createSnapshot(dashboard)
	if err != nil {
		return "", fmt.Errorf("error creating snapshot: %w", err)
	}

	return snapshotURL, nil
}

func getDashboardConfig(dashboardUID string) (map[string]interface{}, error) {
	dashboardURL := fmt.Sprintf("%s/api/dashboards/uid/%s", cfg.GrafanaURL, dashboardUID)
	resp, err := sendRequest("GET", dashboardURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dashboardResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&dashboardResp); err != nil {
		return nil, fmt.Errorf("error decoding dashboard response: %w", err)
	}

	dashboard, ok := dashboardResp["dashboard"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("dashboard data not found in response")
	}

	return dashboard, nil
}

func createSnapshot(dashboard map[string]interface{}) (string, error) {
	snapshotURL := fmt.Sprintf("%s/api/snapshots", cfg.GrafanaURL)
	payload := map[string]interface{}{
		"dashboard": dashboard,
		"expires":   3600, // expires in 1 hour
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshalling snapshot payload: %w", err)
	}

	resp, err := sendRequest("POST", snapshotURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error decoding snapshot response: %w", err)
	}

	snapshotURL, ok := result["url"].(string)
	if !ok {
		return "", fmt.Errorf("snapshot URL not found in response")
	}

	return snapshotURL, nil
}

func sendRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+cfg.GrafanaAPIKey)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

func sendEmailWithSnapshotLink(subject, body, snapshotURL string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.SenderEmail)
	m.SetHeader("To", cfg.RecipientEmail)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", fmt.Sprintf("%s<br><br>Grafana Snapshot: <a href='%s'>View Snapshot</a>", body, snapshotURL))

	d := gomail.NewDialer(cfg.SMTPServer, cfg.SMTPPort, cfg.SenderEmail, cfg.SenderPassword)
	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func handleAlert(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received %s request to %s", r.Method, r.URL.Path)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("error reading request body: %v", err)
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
	log.Printf("request body:\n%s", string(body))

	var alertMessage AlertMessage
	if err := json.Unmarshal(body, &alertMessage); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, alert := range alertMessage.Alerts {
		log.Printf("received alert: %s", alert.Annotations["summary"])

		snapshotURL, err := createGrafanaSnapshot(cfg.DashboardUID)
		if err != nil {
			log.Printf("error creating Grafana snapshot: %v", err)
			snapshotURL = "failed to get snapshot URL"
		}

		subject := fmt.Sprintf("Monitor Alert: %s", alert.Annotations["summary"])
		body := fmt.Sprintf("<h1>%s</h1><p>%s</p>", alert.Annotations["summary"], alert.Annotations["description"])

		if err := sendEmailWithSnapshotLink(subject, body, snapshotURL); err != nil {
			log.Printf("error sending email: %v", err)
			http.Error(w, "Eerror sending email", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	log.Println("alert processing completed")
}

func main() {
	http.HandleFunc("/alert", handleAlert)
	log.Printf("Starting server on port %s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}
