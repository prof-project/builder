PROTO_DIR = proto
GEN_DIR = profpb

all: generate

generate:
	@echo "Generating Go protobuf files..."
	protoc --proto_path=$(PROTO_DIR) --go_out=paths=source_relative:$(GEN_DIR) --go-grpc_out=paths=source_relative:$(GEN_DIR) $(PROTO_DIR)/*.proto
	@echo "Generated protobuf files in $(GEN_DIR)."

clean:
	@echo "Cleaning up generated files..."
	rm -rf $(GEN_DIR)/*.pb.go $(GEN_DIR)/*_grpc.pb.go
	@echo "Cleaned $(GEN_DIR) directory."

.PHONY: all generate clean
