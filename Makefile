.PHONY: run test vet build check clean

run:
	go run .

test:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -o ceksepet-api .

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...
	go build -o ceksepet-api .

clean:
	go clean
	rm -f ceksepet-api coverage.out
