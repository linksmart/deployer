# LinkSmart Deployer
[![GoDoc](https://godoc.org/github.com/linksmart/deployer?status.svg)](https://godoc.org/github.com/linksmart/deployer)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/linksmart/deployer.svg)](https://github.com/linksmart/deployer/tags)
[![CICD](https://github.com/linksmart/deployer/workflows/CICD/badge.svg)](https://github.com/linksmart/deployer/actions?query=workflow%3ACICD)

The LinkSmart Deployer is a lightweight software deployment system for IoT devices. It aims to provide secure, practical, and easy to use utilities for over-the-air (OTA) provisioning of software on single-board computers (e.g. Raspberry Pi). The project is currently under active development.

![LinkSmart Deployer - Conceptual Diagram](https://raw.githubusercontent.com/wiki/linksmart/deployer/figures/deployment-tool-concept-v3.jpg)

## Getting Started
* Documentation: [wiki](https://github.com/linksmart/deployer/wiki) | [API doc - Swagger UI](https://linksmart.github.io/swagger-ui/dist/?url=https://raw.githubusercontent.com/linksmart/deployer/master/apidoc/openapi.yaml)
* :star: Deployment GUI: [source code](https://github.com/linksmart/deployer-ui) | [wiki](https://github.com/linksmart/deployer-ui/wiki)

## Deployment
Snapshots and release artifacts are built using Github Actions. The releases are listed [here](https://github.com/linksmart/deployer/releases).

### Docker
Docker compose scripts are available for [Deployment Manager](https://github.com/linksmart/deployer/blob/master/manager/docker-compose.yml) and dummy [Deployment Agents](https://github.com/linksmart/deployer/blob/master/agent/docker-compose.yml).
### Install Debian Package
Download the suitable debian package and install. E.g.:
```bash
sudo apt install ./deployment-agent-linux-arm.deb
```

### Compile from source
Within the root of the repository:
```bash
go build -o bin/manager ./manager
go build -o bin/agent  ./agent
```
#### Build with static linking
```bash
CGO_CPPFLAGS="-I/usr/include" CGO_LDFLAGS="-L/usr/lib -lzmq -lpthread -lrt -lstdc++ -lm -lc -lgcc" go build -v --ldflags '-extldflags "-static"' -a -o bin/agent ./agent
```
#### Compile using Go < 1.11
```bash
git clone <repo-addr> src/code.linksmart.eu/dt/deployment-tool
export GOPATH=$(pwd)
go build -v code.linksmart.eu/dt/deployment-tool/agent
```

## Development
### Run tests
Locally:
```bash
 go test ./tests -v -failfast
```
In a docker container:
```bash
docker network create test-network
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v $(pwd):$(pwd) -w $(pwd) --network=test-network -e EXTERNAL-NETWORK=test-network golang:1.12 go test ./tests -v -failfast
docker network remove test-network
```

### Dependencies
* [ZeroMQ v4.x.x](http://zeromq.org/intro:get-the-software).  
Runtime: libzmq5, Build: libzmq3-dev

## Contributing
Contributions are welcome. 

Please fork, make your changes, and submit a pull request. For major changes, please open an issue first and discuss it with the other authors.
