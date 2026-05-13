sudo rm /opt/conner1/conner /opt/conner2/conner /opt/conner-server/conner conner 
CGO_ENABLED=1 go build -o conner ./cmd/conner/main.go
cp conner /opt/conner1  && cp conner /opt/conner2 && cp conner /opt/conner-server