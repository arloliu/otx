package scenario

// EdgeIoTScenario returns the edge device management scenario.
// Simulates: device-gateway → device-registry/telemetry-processor → databases
func EdgeIoTScenario() *Scenario {
	return &Scenario{
		Name:        "edge-iot",
		Description: "Edge device telemetry processing with time-series database and rule engine",
		Services: []Service{
			{Name: "device-gateway"},
			{Name: "device-registry"},
			{Name: "telemetry-processor"},
			{Name: "rule-engine"},
		},
		RootSpan: SpanTemplate{
			Name:     "device/+/telemetry",
			Service:  "device-gateway",
			Kind:     SpanKindConsumer,
			Duration: Duration(35_000_000), // 35ms
			Attributes: map[string]string{
				"messaging.system":           "mqtt",
				"messaging.destination.name": "device/+/telemetry",
				"messaging.operation.name":   "receive",
			},
			Logs: []LogTemplate{
				{
					Level:      "DEBUG",
					Message:    "Received telemetry batch",
					Attributes: map[string]string{"batch.size": "10"},
				},
			},
			Children: []SpanTemplate{
				{
					Name:     "ValidateDevice",
					Service:  "device-registry",
					Kind:     SpanKindClient,
					Duration: Duration(8_000_000), // 8ms
					Attributes: map[string]string{
						"rpc.system":  "grpc",
						"rpc.service": "DeviceRegistry",
						"rpc.method":  "ValidateDevice",
					},
					Children: []SpanTemplate{
						{
							Name:     "GET device:123",
							Service:  "device-registry",
							Kind:     SpanKindClient,
							Duration: Duration(2_000_000), // 2ms
							Attributes: map[string]string{
								"db.system":     "redis",
								"db.namespace":  "devices",
								"db.query.text": "GET device:123",
							},
						},
					},
				},
				{
					Name:     "ProcessBatch",
					Service:  "telemetry-processor",
					Kind:     SpanKindInternal,
					Duration: Duration(20_000_000), // 20ms
					Logs: []LogTemplate{
						{Level: "INFO", Message: "Processing telemetry batch"},
					},
					Children: []SpanTemplate{
						{
							Name:     "INSERT metrics",
							Service:  "telemetry-processor",
							Kind:     SpanKindClient,
							Duration: Duration(12_000_000), // 12ms
							Attributes: map[string]string{
								"db.system":     "timescaledb",
								"db.namespace":  "telemetry",
								"db.query.text": "INSERT INTO metrics ...",
							},
						},
						{
							Name:     "EvaluateAlerts",
							Service:  "rule-engine",
							Kind:     SpanKindClient,
							Duration: Duration(5_000_000), // 5ms
							Attributes: map[string]string{
								"rpc.system":  "grpc",
								"rpc.service": "RuleEngine",
								"rpc.method":  "EvaluateAlerts",
							},
							Logs: []LogTemplate{
								{Level: "DEBUG", Message: "Evaluated 3 rules, 0 alerts triggered"},
							},
						},
					},
				},
			},
		},
	}
}
