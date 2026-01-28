package scenario

// EcommerceScenario returns the e-commerce order flow scenario.
// Simulates: api-gateway → order-service → inventory/pricing → database/event-bus
func EcommerceScenario() *Scenario {
	return &Scenario{
		Name:        "ecommerce",
		Description: "E-commerce order creation with inventory reservation and event publishing",
		Services: []Service{
			{Name: "api-gateway"},
			{Name: "order-service"},
			{Name: "inventory-service"},
			{Name: "pricing-service"},
		},
		RootSpan: SpanTemplate{
			Name:     "POST /orders",
			Service:  "api-gateway",
			Kind:     SpanKindServer,
			Duration: Duration(120_000_000), // 120ms
			Attributes: map[string]string{
				"http.request.method":       "POST",
				"http.route":                "/orders",
				"url.path":                  "/orders",
				"http.response.status_code": "201",
			},
			Logs: []LogTemplate{
				{Level: "INFO", Message: "Order creation request received"},
			},
			Children: []SpanTemplate{
				{
					Name:     "CreateOrder",
					Service:  "order-service",
					Kind:     SpanKindInternal,
					Duration: Duration(100_000_000), // 100ms
					Attributes: map[string]string{
						"order.items_count": "3",
					},
					Children: []SpanTemplate{
						{
							Name:     "ReserveStock",
							Service:  "inventory-service",
							Kind:     SpanKindClient,
							Duration: Duration(25_000_000), // 25ms
							Attributes: map[string]string{
								"rpc.system":  "grpc",
								"rpc.service": "InventoryService",
								"rpc.method":  "ReserveStock",
							},
							ErrorRate:   0.02, // 2% out of stock
							ErrorStatus: "insufficient stock",
							Children: []SpanTemplate{
								{
									Name:     "SELECT stock",
									Service:  "inventory-service",
									Kind:     SpanKindClient,
									Duration: Duration(8_000_000), // 8ms
									Attributes: map[string]string{
										"db.system":     "postgresql",
										"db.namespace":  "inventory",
										"db.query.text": "SELECT available_qty FROM stock WHERE sku IN (...)",
									},
								},
							},
						},
						{
							Name:     "CalculateTotal",
							Service:  "pricing-service",
							Kind:     SpanKindClient,
							Duration: Duration(15_000_000), // 15ms
							Attributes: map[string]string{
								"rpc.system":  "grpc",
								"rpc.service": "PricingService",
								"rpc.method":  "CalculateTotal",
							},
							Logs: []LogTemplate{
								{
									Level:      "DEBUG",
									Message:    "Applied discount code",
									Attributes: map[string]string{"discount.percent": "10"},
								},
							},
						},
						{
							Name:     "INSERT order",
							Service:  "order-service",
							Kind:     SpanKindClient,
							Duration: Duration(18_000_000), // 18ms
							Attributes: map[string]string{
								"db.system":     "postgresql",
								"db.namespace":  "orders",
								"db.query.text": "INSERT INTO orders (...) VALUES (...)",
							},
						},
					},
				},
				{
					Name:     "order.created",
					Service:  "order-service",
					Kind:     SpanKindProducer,
					Duration: Duration(5_000_000), // 5ms
					Attributes: map[string]string{
						"messaging.system":           "nats",
						"messaging.destination.name": "order.created",
						"messaging.operation.name":   "publish",
					},
				},
			},
		},
	}
}

// HealthCheckScenario returns a simple health check scenario.
// Useful for testing OTLP connectivity.
func HealthCheckScenario() *Scenario {
	return &Scenario{
		Name:        "health-check",
		Description: "Simple HTTP health check for testing OTLP connectivity",
		Services: []Service{
			{Name: "health-service"},
		},
		RootSpan: SpanTemplate{
			Name:     "GET /health",
			Service:  "health-service",
			Kind:     SpanKindServer,
			Duration: Duration(5_000_000), // 5ms
			Attributes: map[string]string{
				"http.request.method":       "GET",
				"http.route":                "/health",
				"url.path":                  "/health",
				"http.response.status_code": "200",
			},
			Logs: []LogTemplate{
				{Level: "INFO", Message: "Health check passed"},
			},
		},
	}
}
