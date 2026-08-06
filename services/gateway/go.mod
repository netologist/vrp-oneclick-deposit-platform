module github.com/hozgan/vrp-demo/services/gateway

go 1.26.2

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/hozgan/vrp-demo/gen v0.0.0
	github.com/hozgan/vrp-demo/pkg/shared v0.0.0
	github.com/redis/go-redis/v9 v9.7.3
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace (
	github.com/hozgan/vrp-demo/gen => ../../gen
	github.com/hozgan/vrp-demo/pkg/shared => ../../pkg/shared
)
