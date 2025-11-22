.PHONY: build-server build-client run-server run-client clean

build-server:
	@echo "Building server..."
	cd server && go build -o ../bin/marmotmaster-server main.go
	@echo "Copying static files..."
	@mkdir -p bin/static
	@cp -r server/static/* bin/static/
	@echo "Server build complete!"

build-client:
	@echo "Building client..."
	cd client && go build -o ../bin/marmotmaster-client main.go
	@echo "Client build complete!"

build: build-server build-client

run-server: build-server
	cd bin && ./marmotmaster-server

run-client: build-client
	./bin/marmotmaster-client

clean:
	rm -rf bin/

deps:
	go mod download

