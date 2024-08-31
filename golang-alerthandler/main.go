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

func getGrafanaChart(dashboardUID, panelID string) ([]byte, error) {
	url := fmt.Sprintf("%s/render/d-solo/%s?orgId=1&panelId=%s&width=1000&height=500", grafanaURL, dashboardUID, panelID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+grafanaAPIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func sendEmailWithChart(subject, body string, chartData []byte) error {
	log.Println("Preparing to send email...")
	log.Printf("Subject: %s", subject)
	log.Printf("Body: %s", body)
	log.Printf("Chart data length: %d bytes", len(chartData))

	m := gomail.NewMessage()
	m.SetHeader("From", senderEmail)
	m.SetHeader("To", recipientEmail)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)
	m.Attach("chart.png", gomail.SetCopyFunc(func(w io.Writer) error {
		_, err := w.Write(chartData)
		return err
	}))

	log.Printf("Sending email from %s to %s", senderEmail, recipientEmail)
	log.Printf("Using SMTP server: %s:%d", smtpServer, smtpPort)

	d := gomail.NewDialer(smtpServer, smtpPort, senderEmail, senderPassword)
	err := d.DialAndSend(m)
	if err != nil {
		log.Printf("Error sending email: %v", err)
		return err
	}

	log.Println("Email sent successfully")
	return nil
}

func handleAlert(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received %s request to %s", r.Method, r.URL.Path)

	log.Println("Request Headers:")
	for name, values := range r.Header {
		for _, value := range values {
			log.Printf("%s: %s", name, value)
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	log.Printf("Request Body:\n%s", string(body))

	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var alertMessage AlertMessage
	if err := json.NewDecoder(r.Body).Decode(&alertMessage); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, alert := range alertMessage.Alerts {
		log.Printf("Received alert: %s\n", alert.Annotations["summary"])
		
		dashboardUID := os.Getenv("DASHBOARD_UID")
		panelID := os.Getenv("PANEL_ID")

		log.Printf("Fetching Grafana chart for dashboard UID: %s, panel ID: %s", dashboardUID, panelID)
		chartData, err := getGrafanaChart(dashboardUID, panelID)
		if err != nil {
			log.Printf("Error getting Grafana chart: %v\n", err)
			continue
		}
		log.Printf("Successfully fetched Grafana chart, size: %d bytes", len(chartData))

		subject := fmt.Sprintf("Alert: %s", alert.Annotations["summary"])
		body := fmt.Sprintf("<h1>%s</h1><p>%s</p>", alert.Annotations["summary"], alert.Annotations["description"])
		if err := sendEmailWithChart(subject, body, chartData); err != nil {
			log.Printf("Error sending email: %v\n", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	log.Println("Alert processing completed")
}

func main() {
	port := os.Getenv("PORT")
	http.HandleFunc("/alert", handleAlert)
	log.Printf("Starting server on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}