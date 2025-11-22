.PHONY: build-server build-client run-server run-client clean \
	build-client-windows build-server-windows build-windows \
	build-client-windows-32 build-server-windows-32 build-windows-32 \
	build-client-darwin build-server-darwin build-darwin \
	build-client-darwin-arm64 build-server-darwin-arm64 build-darwin-arm64 \
	build-all

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

# Build all platform variants
build-all: build build-windows build-windows-32 build-darwin build-darwin-arm64
	@echo "All platform builds complete!"

# Windows builds (64-bit)
build-client-windows:
	@echo "Building Windows client (64-bit)..."
	cd client && GOOS=windows GOARCH=amd64 go build -o ../bin/marmotmaster-client.exe main.go
	@echo "Windows client build complete!"

build-server-windows:
	@echo "Building Windows server (64-bit)..."
	cd server && GOOS=windows GOARCH=amd64 go build -o ../bin/marmotmaster-server.exe main.go
	@echo "Copying static files..."
	@mkdir -p bin/static
	@cp -r server/static/* bin/static/
	@echo "Windows server build complete!"

build-windows: build-server-windows build-client-windows

# Windows builds (32-bit)
build-client-windows-32:
	@echo "Building Windows client (32-bit)..."
	cd client && GOOS=windows GOARCH=386 go build -o ../bin/marmotmaster-client-32.exe main.go
	@echo "Windows client (32-bit) build complete!"

build-server-windows-32:
	@echo "Building Windows server (32-bit)..."
	cd server && GOOS=windows GOARCH=386 go build -o ../bin/marmotmaster-server-32.exe main.go
	@echo "Copying static files..."
	@mkdir -p bin/static
	@cp -r server/static/* bin/static/
	@echo "Windows server (32-bit) build complete!"

build-windows-32: build-server-windows-32 build-client-windows-32

# macOS builds (Intel/amd64)
build-client-darwin:
	@echo "Building macOS client (Intel)..."
	cd client && GOOS=darwin GOARCH=amd64 go build -o ../bin/marmotmaster-client-darwin-amd64 main.go
	@echo "macOS client (Intel) build complete!"

build-server-darwin:
	@echo "Building macOS server (Intel)..."
	cd server && GOOS=darwin GOARCH=amd64 go build -o ../bin/marmotmaster-server-darwin-amd64 main.go
	@echo "Copying static files..."
	@mkdir -p bin/static
	@cp -r server/static/* bin/static/
	@echo "macOS server (Intel) build complete!"

build-darwin: build-server-darwin build-client-darwin

# macOS builds (Apple Silicon/arm64)
build-client-darwin-arm64:
	@echo "Building macOS client (Apple Silicon)..."
	cd client && GOOS=darwin GOARCH=arm64 go build -o ../bin/marmotmaster-client-darwin-arm64 main.go
	@echo "macOS client (Apple Silicon) build complete!"

build-server-darwin-arm64:
	@echo "Building macOS server (Apple Silicon)..."
	cd server && GOOS=darwin GOARCH=arm64 go build -o ../bin/marmotmaster-server-darwin-arm64 main.go
	@echo "Copying static files..."
	@mkdir -p bin/static
	@cp -r server/static/* bin/static/
	@echo "macOS server (Apple Silicon) build complete!"

build-darwin-arm64: build-server-darwin-arm64 build-client-darwin-arm64

run-server: build-server
	cd bin && ./marmotmaster-server

run-client: build-client
	./bin/marmotmaster-client

clean:
	rm -rf bin/

deps:
	go mod download

