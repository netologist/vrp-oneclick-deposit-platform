module github.com/hozgan/vrp-demo/services/bank-adapter

go 1.26.2

require (
	github.com/avast/retry-go/v4 v4.6.1
	github.com/google/uuid v1.6.0
	github.com/hozgan/vrp-demo/gen v0.0.0
	github.com/hozgan/vrp-demo/pkg/shared v0.0.0
	github.com/sony/gobreaker v1.0.0
	google.golang.org/protobuf v1.36.11
)

require (
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
