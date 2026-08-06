module github.com/hozgan/vrp-demo/services/notification-svc

go 1.26.2

require (
	github.com/hozgan/vrp-demo/gen v0.0.0
	github.com/hozgan/vrp-demo/pkg/shared v0.0.0
	github.com/redis/go-redis/v9 v9.7.3
	github.com/segmentio/kafka-go v0.4.47
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/hozgan/vrp-demo/gen => ../../gen
	github.com/hozgan/vrp-demo/pkg/shared => ../../pkg/shared
)
