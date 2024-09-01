# Prometheus and Grafana Monitoring System
This project demonstrates the use of Prometheus and Grafana for monitoring, with additional components for alerting and handling alerts.

## Prerequisites
* Docker and Docker Compose
* Go 1.21 or later (for local development of the alert handler)

## Components
* Node Exporter: Collects system metrics
* Prometheus: Time series database and monitoring system
* Grafana: Visualization and analytics platform
* Alertmanager: Handles alerts from Prometheus
* Golang Alert Handler: Custom service to process and forward alerts

## Setup

###  1. Clone:
```
mkdir monitor
cd monitor
git clone https://github.com/hugohu0224/system-monitor.git
```

### 2. Adjust .env
```
// change name to .env
mv .env.example .env

// adjust related variables
vim .env

// variables note
GRAFANA_API_KEY: Generate an API key after creating a service account from grafana.
DASHBOARD_UID: After creating a dashboard, you can view it from the JSON Model.
SENDER_PASSWORD: This is a application password from Gmail, you need to create it.
```

### 3.Build and start the services:
```
// make sure you're in the /monitor
docker-compose up -d
```

### 4. Check if the service is started

![dockerup](/photos/dockerup.png)
![dockerps](/photos/dockerps.png)

## Usage & Showcase
### Service access
* Access Grafana at http://localhost:3000 and log in with the credentials specified in the .env file.
* Access Prometheus at http://localhost:9090 to query metrics and view configured alerts.
* Access Alertmanager at http://localhost:9093 to view and manage alerts.

### Grafana
#### 1. API Key (Token)
![token](/photos/apikey.png)
#### 2. Dashboard
![token](/photos/dashboard.png)

### Alert rules
The configuration in *alert_rules.yml* will appear on the alert page in Prometheus,
and we also need *prometheus/alertmanager.yml* to define where the alerts will be sent.
![alertmanager](/photos/alertmanager.png)

### Alert sent to Gmail
* #### title
![email](/photos/email.png)
* #### content
![emailcontent](/photos/emailcontent.png)
* #### snapshot (hash code)
![snapshot](/photos/snapshot.png)
