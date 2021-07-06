generate: generate/proto

generate/proto:
	protoc -Iproto ./proto/scanner.proto --go_out=plugins=grpc:proto
