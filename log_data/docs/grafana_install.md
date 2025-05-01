# Grafana Installation on Raspberry Pi 5

This guide installs Grafana on Raspberry Pi OS (Debian-based) and configures it to connect with InfluxDB v2.

## Prerequisites
- Raspberry Pi 5 running Raspberry Pi OS (64-bit recommended)
- sudo/root access
- Internet connection

## Installation Steps

### 1. Update System Packages
```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y apt-transport-https software-properties-common wget

# Create keyring directory only if it doesn't exist
sudo mkdir -p /etc/apt/keyrings

# Download and install GPG key
curl -fsSL https://packages.grafana.com/gpg.key | sudo gpg --dearmor -o /etc/apt/keyrings/grafana.gpg

# Add repository to sources
echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://packages.grafana.com/oss/deb stable main" | sudo tee /etc/apt/sources.list.d/grafana.list
sudo apt update
sudo apt install grafana -y

sudo systemctl daemon-reload
sudo systemctl start grafana-server
sudo systemctl enable grafana-server

sudo systemctl status grafana-server
