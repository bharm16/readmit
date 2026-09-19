.PHONY: package test
package:
	rm -rf build/package
	mkdir -p build/package
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o build/package/readmit-hub ./cmd/readmit-hub
	cp README.md config.example.json readmit-hub.service build/package/
	cp -R licenses build/package/
	tar -czf build/readmit-hub-linux-amd64.tar.gz -C build/package .

test:
	go test -race ./...
	go vet ./...
