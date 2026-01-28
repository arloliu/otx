package scenario

// PaymentScenario returns the online payment system scenario.
// Simulates: gateway → payment-service → fraud-detection/payment-processor → external APIs
func PaymentScenario() *Scenario {
	return &Scenario{
		Name:        "payment",
		Description: "Online payment system with fraud detection and external payment processor",
		Services: []Service{
			{Name: "payment-gateway"},
			{Name: "payment-service"},
			{Name: "fraud-detection"},
			{Name: "ml-service"},
			{Name: "payment-processor"},
			{Name: "notification-service"},
		},
		RootSpan: SpanTemplate{
			Name:     "POST /api/v1/checkout",
			Service:  "payment-gateway",
			Kind:     SpanKindServer,
			Duration: Duration(180_000_000), // 180ms
			Attributes: map[string]string{
				"http.request.method":       "POST",
				"http.route":                "/api/v1/checkout",
				"url.path":                  "/api/v1/checkout",
				"http.response.status_code": "200",
			},
			Children: []SpanTemplate{
				{
					Name:     "ProcessPayment",
					Service:  "payment-service",
					Kind:     SpanKindInternal,
					Duration: Duration(150_000_000), // 150ms
					Attributes: map[string]string{
						"payment.amount":   "99.99",
						"payment.currency": "USD",
					},
					Logs: []LogTemplate{
						{Level: "INFO", Message: "Processing payment request", Delay: Duration(1_000_000)},
					},
					Children: []SpanTemplate{
						{
							Name:     "AnalyzeTransaction",
							Service:  "fraud-detection",
							Kind:     SpanKindClient,
							Duration: Duration(45_000_000), // 45ms
							Attributes: map[string]string{
								"rpc.system":  "grpc",
								"rpc.service": "FraudDetection",
								"rpc.method":  "AnalyzeTransaction",
							},
							Children: []SpanTemplate{
								{
									Name:     "Predict",
									Service:  "ml-service",
									Kind:     SpanKindClient,
									Duration: Duration(25_000_000), // 25ms
									Attributes: map[string]string{
										"rpc.system":  "grpc",
										"rpc.service": "MLService",
										"rpc.method":  "Predict",
										"ml.model":    "fraud-detector-v2",
									},
									Logs: []LogTemplate{
										{
											Level:      "DEBUG",
											Message:    "ML prediction completed",
											Attributes: map[string]string{"ml.score": "0.12"},
										},
									},
								},
							},
						},
						{
							Name:        "ChargeCard",
							Service:     "payment-processor",
							Kind:        SpanKindInternal,
							Duration:    Duration(80_000_000), // 80ms
							ErrorRate:   0.05,                 // 5% error rate
							ErrorStatus: "payment declined",
							Children: []SpanTemplate{
								{
									Name:     "POST /v2/charges",
									Service:  "payment-processor",
									Kind:     SpanKindClient,
									Duration: Duration(65_000_000), // 65ms
									Attributes: map[string]string{
										"http.request.method":       "POST",
										"url.full":                  "https://api.stripe.com/v2/charges",
										"http.response.status_code": "200",
									},
								},
							},
						},
					},
				},
				{
					Name:     "SendConfirmation",
					Service:  "notification-service",
					Kind:     SpanKindProducer,
					Duration: Duration(15_000_000), // 15ms
					Attributes: map[string]string{
						"messaging.system":           "kafka",
						"messaging.destination.name": "notifications",
						"messaging.operation.name":   "publish",
					},
					Logs: []LogTemplate{
						{Level: "INFO", Message: "Confirmation email queued"},
					},
				},
			},
		},
	}
}
