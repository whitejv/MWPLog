# MWPLog
## Installing InfluxDB
    curl https://repos.influxdata.com/influxdata-archive.key | gpg --dearmor | sudo tee /usr/share/keyrings/influxdb-archive-keyring.gpg >/dev/null
    echo "deb [signed-by=/usr/share/keyrings/influxdb-archive-keyring.gpg] https://repos.influxdata.com/debian stable main" | sudo tee /etc/apt/sources.list.d/influxdb.list
    sudo apt-get update
    sudo apt install influxdb2
    sudo systemctl unmask influxdb
    sudo systemctl enable influxdb
    sudo systemctl start influxdb

## Installing GNU Autotools
    sudo apt-get install autoconf automake libtool

## Installing Influxdb C client - initialize your Go module.
    go mod init github.com/xNok/Getting-Started-with-Go-and-InfluxDB
    Add influxdb-client-go as a dependency to your project.
    go get github.com/influxdata/influxdb-client-go/v2

## Installing GO Language
    Go to Website: https://go.dev/dl/
    sudo mv go_version.tar.gz /usr/local
    Download tar file into /usr/local
    tar -C /usr/local -xzf go_version.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    source ~/.bashrc
    go version

## Installing telegraf
    Install Go
    Clone the Telegraf repository:
    git clone https://github.com/influxdata/telegraf.git

    ### Run make build from the source directory
    cd telegraf
    make build

