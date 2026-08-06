module github.com/hozgan/vrp-demo/services/risk-svc

go 1.26.2

require (
	github.com/alicebob/miniredis/v2 v2.34.0
	github.com/google/uuid v1.6.0
	github.com/hozgan/vrp-demo/gen v0.0.0
	github.com/hozgan/vrp-demo/pkg/shared v0.0.0
	github.com/redis/go-redis/v9 v9.7.3
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/alicebob/gopher-json v0.0.0-20230218143504-906a9b012302 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.0 // indirect
)

replace (
	github.com/hozgan/vrp-demo/gen => ../../gen
	github.com/hozgan/vrp-demo/pkg/shared => ../../pkg/shared
)
