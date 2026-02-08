package main

import "encoding/json"

// applogMsg holds a structured application log entry template.
type applogMsg struct {
	level     string // DEBUG, INFO, WARN, ERROR, FATAL
	component string
	msg       string
	source    string          // file:line
	attrs     json.RawMessage // optional structured attributes
}

// serviceProfile groups all data for a simulated Go backend service.
type serviceProfile struct {
	weight  int // relative probability weight
	service string
	msgs    []applogMsg
}

// applogLevelWeight maps a log level to its relative probability.
type applogLevelWeight struct {
	level  string
	weight int
}

// applogLevelWeights produces realistic level distributions.
// INFO: 54.5%, DEBUG: 20%, WARN: 15%, ERROR: 10%, FATAL: 0.5%.
var applogLevelWeights = []applogLevelWeight{
	{"INFO", 445},
	{"DEBUG", 200},
	{"WARN", 150},
	{"ERROR", 100},
	{"FATAL", 5},
}

var services = []serviceProfile{
	authService,
	orderService,
	paymentService,
	notificationService,
	gatewayService,
	inventoryService,
}

// --- auth-service (weight 25) ---

var authService = serviceProfile{
	weight:  25,
	service: "auth-service",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "authenticator", msg: "user authenticated successfully", source: "auth.go:87", attrs: j(`{"user_id":"usr_8f3a","method":"oauth2","provider":"github","duration_ms":142}`)},
		{level: "INFO", component: "authenticator", msg: "user authenticated successfully", source: "auth.go:87", attrs: j(`{"user_id":"usr_2b19","method":"password","duration_ms":45}`)},
		{level: "INFO", component: "authenticator", msg: "user authenticated successfully", source: "auth.go:87", attrs: j(`{"user_id":"usr_c401","method":"api_key","duration_ms":12}`)},
		{level: "INFO", component: "session", msg: "session created", source: "session.go:34", attrs: j(`{"session_id":"sess_a1b2c3","user_id":"usr_8f3a","ttl_hours":24}`)},
		{level: "INFO", component: "session", msg: "session refreshed", source: "session.go:112", attrs: j(`{"session_id":"sess_x7y8z9","user_id":"usr_2b19","remaining_hours":18}`)},
		{level: "INFO", component: "session", msg: "session expired", source: "session.go:145", attrs: j(`{"session_id":"sess_old123","user_id":"usr_d502"}`)},
		{level: "INFO", component: "token", msg: "access token issued", source: "token.go:56", attrs: j(`{"user_id":"usr_8f3a","token_type":"bearer","expires_in":3600}`)},
		{level: "INFO", component: "token", msg: "refresh token rotated", source: "token.go:98", attrs: j(`{"user_id":"usr_c401"}`)},
		// DEBUG
		{level: "DEBUG", component: "middleware", msg: "validating request token", source: "middleware.go:23", attrs: j(`{"path":"/api/v1/users/me","method":"GET"}`)},
		{level: "DEBUG", component: "middleware", msg: "CORS preflight handled", source: "middleware.go:67", attrs: j(`{"origin":"https://app.example.com","method":"POST"}`)},
		{level: "DEBUG", component: "authenticator", msg: "looking up user in cache", source: "auth.go:42", attrs: j(`{"user_id":"usr_8f3a","cache_hit":true}`)},
		{level: "DEBUG", component: "authenticator", msg: "looking up user in cache", source: "auth.go:42", attrs: j(`{"user_id":"usr_new1","cache_hit":false}`)},
		// WARN
		{level: "WARN", component: "authenticator", msg: "login attempt with invalid credentials", source: "auth.go:102", attrs: j(`{"username":"admin","remote_addr":"198.51.100.5","attempt":3}`)},
		{level: "WARN", component: "authenticator", msg: "login attempt with invalid credentials", source: "auth.go:102", attrs: j(`{"username":"root","remote_addr":"198.51.100.12","attempt":5}`)},
		{level: "WARN", component: "token", msg: "token validation failed: expired", source: "token.go:71", attrs: j(`{"user_id":"usr_d502","expired_at":"2025-12-01T00:00:00Z"}`)},
		{level: "WARN", component: "session", msg: "concurrent session limit reached", source: "session.go:89", attrs: j(`{"user_id":"usr_2b19","active_sessions":5,"limit":5}`)},
		{level: "WARN", component: "ratelimit", msg: "rate limit approaching threshold", source: "ratelimit.go:44", attrs: j(`{"remote_addr":"198.51.100.5","current":95,"limit":100,"window":"1m"}`)},
		// ERROR
		{level: "ERROR", component: "authenticator", msg: "account locked after repeated failures", source: "auth.go:120", attrs: j(`{"username":"admin","remote_addr":"198.51.100.5","failures":10,"locked_until":"2026-02-04T13:00:00Z"}`)},
		{level: "ERROR", component: "session", msg: "failed to persist session to redis", source: "session.go:51", attrs: j(`{"error":"dial tcp 10.0.5.10:6379: connection refused","session_id":"sess_fail1"}`)},
		{level: "ERROR", component: "token", msg: "failed to sign token", source: "token.go:63", attrs: j(`{"error":"private key not found","key_id":"kid_2024"}`)},
		// FATAL
		{level: "FATAL", component: "token", msg: "unable to load signing keys, shutting down", source: "token.go:15", attrs: j(`{"error":"open /etc/auth/keys: no such file or directory"}`)},
	},
}

// --- order-service (weight 25) ---

var orderService = serviceProfile{
	weight:  25,
	service: "order-service",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "handler", msg: "order created", source: "order_handler.go:45", attrs: j(`{"order_id":"ord_9f2a","user_id":"usr_8f3a","items":3,"total_cents":15990}`)},
		{level: "INFO", component: "handler", msg: "order confirmed", source: "order_handler.go:102", attrs: j(`{"order_id":"ord_9f2a","payment_id":"pay_x1y2"}`)},
		{level: "INFO", component: "handler", msg: "order shipped", source: "order_handler.go:134", attrs: j(`{"order_id":"ord_7b3c","tracking_number":"1Z999AA10123456784","carrier":"ups"}`)},
		{level: "INFO", component: "handler", msg: "order delivered", source: "order_handler.go:156", attrs: j(`{"order_id":"ord_5e1d","delivered_at":"2026-02-04T14:30:00Z"}`)},
		{level: "INFO", component: "handler", msg: "order cancelled by user", source: "order_handler.go:178", attrs: j(`{"order_id":"ord_3c2b","user_id":"usr_2b19","reason":"changed_mind"}`)},
		{level: "INFO", component: "fulfillment", msg: "fulfillment request queued", source: "fulfillment.go:28", attrs: j(`{"order_id":"ord_9f2a","warehouse":"wh-east-01"}`)},
		{level: "INFO", component: "fulfillment", msg: "fulfillment completed", source: "fulfillment.go:67", attrs: j(`{"order_id":"ord_7b3c","warehouse":"wh-west-02","pick_duration_ms":45200}`)},
		// DEBUG
		{level: "DEBUG", component: "handler", msg: "validating order request", source: "order_handler.go:30", attrs: j(`{"user_id":"usr_8f3a","item_count":3}`)},
		{level: "DEBUG", component: "store", msg: "executing query", source: "order_store.go:89", attrs: j(`{"query":"SELECT","duration_ms":2}`)},
		{level: "DEBUG", component: "store", msg: "executing query", source: "order_store.go:89", attrs: j(`{"query":"INSERT","duration_ms":5}`)},
		{level: "DEBUG", component: "events", msg: "publishing event to nats", source: "events.go:15", attrs: j(`{"subject":"orders.created","order_id":"ord_9f2a"}`)},
		// WARN
		{level: "WARN", component: "handler", msg: "order total exceeds review threshold", source: "order_handler.go:55", attrs: j(`{"order_id":"ord_big1","total_cents":500000,"threshold_cents":250000}`)},
		{level: "WARN", component: "fulfillment", msg: "warehouse capacity low", source: "fulfillment.go:42", attrs: j(`{"warehouse":"wh-east-01","utilization_pct":92}`)},
		{level: "WARN", component: "store", msg: "slow query detected", source: "order_store.go:95", attrs: j(`{"query":"SELECT","duration_ms":1250,"threshold_ms":500}`)},
		{level: "WARN", component: "handler", msg: "duplicate order submission detected", source: "order_handler.go:38", attrs: j(`{"idempotency_key":"idk_abc123","user_id":"usr_c401"}`)},
		// ERROR
		{level: "ERROR", component: "handler", msg: "failed to create order", source: "order_handler.go:48", attrs: j(`{"error":"insufficient inventory for SKU-1234","user_id":"usr_8f3a","sku":"SKU-1234"}`)},
		{level: "ERROR", component: "fulfillment", msg: "fulfillment request failed", source: "fulfillment.go:35", attrs: j(`{"order_id":"ord_fail1","error":"warehouse wh-east-01 unreachable","retry_count":3}`)},
		{level: "ERROR", component: "events", msg: "failed to publish event", source: "events.go:22", attrs: j(`{"error":"nats: no responders","subject":"orders.created"}`)},
		{level: "ERROR", component: "store", msg: "transaction failed", source: "order_store.go:110", attrs: j(`{"error":"pq: deadlock detected","order_id":"ord_dead1"}`)},
		// FATAL
		{level: "FATAL", component: "store", msg: "database connection pool exhausted, shutting down", source: "order_store.go:25", attrs: j(`{"error":"pq: too many connections for role \"order_svc\"","active":100,"max":100}`)},
	},
}

// --- payment-service (weight 20) ---

var paymentService = serviceProfile{
	weight:  20,
	service: "payment-service",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "processor", msg: "payment processed successfully", source: "processor.go:78", attrs: j(`{"payment_id":"pay_x1y2","amount_cents":15990,"currency":"USD","method":"card","last4":"4242"}`)},
		{level: "INFO", component: "processor", msg: "payment processed successfully", source: "processor.go:78", attrs: j(`{"payment_id":"pay_a3b4","amount_cents":4500,"currency":"EUR","method":"card","last4":"1234"}`)},
		{level: "INFO", component: "processor", msg: "refund issued", source: "processor.go:134", attrs: j(`{"payment_id":"pay_old1","refund_id":"ref_r1s2","amount_cents":9990,"reason":"item_returned"}`)},
		{level: "INFO", component: "webhook", msg: "stripe webhook processed", source: "webhook.go:45", attrs: j(`{"event_type":"charge.succeeded","event_id":"evt_1234"}`)},
		{level: "INFO", component: "webhook", msg: "stripe webhook processed", source: "webhook.go:45", attrs: j(`{"event_type":"payment_intent.created","event_id":"evt_5678"}`)},
		{level: "INFO", component: "ledger", msg: "ledger entry recorded", source: "ledger.go:33", attrs: j(`{"payment_id":"pay_x1y2","type":"credit","amount_cents":15990}`)},
		// DEBUG
		{level: "DEBUG", component: "processor", msg: "initiating payment with provider", source: "processor.go:55", attrs: j(`{"provider":"stripe","amount_cents":15990,"idempotency_key":"idk_pay_001"}`)},
		{level: "DEBUG", component: "processor", msg: "3DS authentication not required", source: "processor.go:62", attrs: j(`{"payment_id":"pay_x1y2","risk_score":12}`)},
		{level: "DEBUG", component: "webhook", msg: "verifying webhook signature", source: "webhook.go:30", attrs: j(`{"endpoint":"/webhooks/stripe"}`)},
		// WARN
		{level: "WARN", component: "processor", msg: "payment requires 3DS authentication", source: "processor.go:65", attrs: j(`{"payment_id":"pay_3ds1","risk_score":78,"threshold":50}`)},
		{level: "WARN", component: "processor", msg: "payment retry scheduled", source: "processor.go:95", attrs: j(`{"payment_id":"pay_retry1","attempt":2,"max_attempts":3,"reason":"soft_decline"}`)},
		{level: "WARN", component: "webhook", msg: "duplicate webhook event received", source: "webhook.go:52", attrs: j(`{"event_id":"evt_1234","event_type":"charge.succeeded"}`)},
		{level: "WARN", component: "fraud", msg: "transaction flagged for review", source: "fraud.go:88", attrs: j(`{"payment_id":"pay_sus1","risk_score":91,"rules_triggered":["velocity_check","geo_mismatch"]}`)},
		// ERROR
		{level: "ERROR", component: "processor", msg: "payment declined by issuer", source: "processor.go:82", attrs: j(`{"payment_id":"pay_dec1","decline_code":"insufficient_funds","last4":"9999"}`)},
		{level: "ERROR", component: "processor", msg: "payment provider timeout", source: "processor.go:88", attrs: j(`{"error":"context deadline exceeded","provider":"stripe","timeout_ms":5000}`)},
		{level: "ERROR", component: "webhook", msg: "webhook signature verification failed", source: "webhook.go:35", attrs: j(`{"error":"signature mismatch","remote_addr":"198.51.100.50"}`)},
		{level: "ERROR", component: "ledger", msg: "ledger reconciliation mismatch", source: "ledger.go:67", attrs: j(`{"expected_cents":15990,"actual_cents":14990,"payment_id":"pay_x1y2"}`)},
		// FATAL
		{level: "FATAL", component: "processor", msg: "payment processor circuit breaker open, shutting down", source: "processor.go:20", attrs: j(`{"provider":"stripe","consecutive_failures":50,"threshold":25}`)},
	},
}

// --- notification-service (weight 10) ---

var notificationService = serviceProfile{
	weight:  10,
	service: "notification-service",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "email", msg: "email sent", source: "email.go:45", attrs: j(`{"to":"user@example.com","template":"order_confirmation","message_id":"msg_e001"}`)},
		{level: "INFO", component: "email", msg: "email sent", source: "email.go:45", attrs: j(`{"to":"admin@example.com","template":"alert_notification","message_id":"msg_e002"}`)},
		{level: "INFO", component: "sms", msg: "sms delivered", source: "sms.go:38", attrs: j(`{"phone":"+1555****890","template":"otp_code","provider":"twilio"}`)},
		{level: "INFO", component: "push", msg: "push notification sent", source: "push.go:52", attrs: j(`{"user_id":"usr_8f3a","platform":"ios","topic":"order_update"}`)},
		{level: "INFO", component: "worker", msg: "notification batch processed", source: "worker.go:90", attrs: j(`{"batch_size":50,"duration_ms":320,"success":48,"failed":2}`)},
		// DEBUG
		{level: "DEBUG", component: "email", msg: "rendering email template", source: "email.go:30", attrs: j(`{"template":"order_confirmation","locale":"en-US"}`)},
		{level: "DEBUG", component: "worker", msg: "polling notification queue", source: "worker.go:22", attrs: j(`{"queue":"notifications","pending":12}`)},
		// WARN
		{level: "WARN", component: "email", msg: "email soft bounce", source: "email.go:58", attrs: j(`{"to":"bounce@example.com","bounce_type":"mailbox_full","retry":true}`)},
		{level: "WARN", component: "sms", msg: "sms delivery delayed", source: "sms.go:55", attrs: j(`{"phone":"+1555****123","status":"queued","provider_delay_ms":8500}`)},
		{level: "WARN", component: "worker", msg: "notification queue depth high", source: "worker.go:35", attrs: j(`{"queue":"notifications","depth":5000,"threshold":3000}`)},
		// ERROR
		{level: "ERROR", component: "email", msg: "email delivery failed", source: "email.go:62", attrs: j(`{"to":"invalid@bad-domain.invalid","error":"550 5.1.1 user unknown","template":"password_reset"}`)},
		{level: "ERROR", component: "sms", msg: "sms provider error", source: "sms.go:48", attrs: j(`{"error":"twilio: 21211 invalid phone number","phone":"+0000000000"}`)},
		{level: "ERROR", component: "push", msg: "push notification failed", source: "push.go:60", attrs: j(`{"error":"apns: 410 Unregistered","user_id":"usr_gone1","platform":"ios"}`)},
		// FATAL
		{level: "FATAL", component: "worker", msg: "message queue connection lost, shutting down", source: "worker.go:18", attrs: j(`{"error":"amqp: connection reset by peer","broker":"rabbitmq-01.internal:5672"}`)},
	},
}

// --- api-gateway (weight 15) ---

var gatewayService = serviceProfile{
	weight:  15,
	service: "api-gateway",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "router", msg: "request handled", source: "router.go:112", attrs: j(`{"method":"GET","path":"/api/v1/orders","status":200,"duration_ms":45,"bytes":2048}`)},
		{level: "INFO", component: "router", msg: "request handled", source: "router.go:112", attrs: j(`{"method":"POST","path":"/api/v1/orders","status":201,"duration_ms":120,"bytes":512}`)},
		{level: "INFO", component: "router", msg: "request handled", source: "router.go:112", attrs: j(`{"method":"GET","path":"/api/v1/users/me","status":200,"duration_ms":18,"bytes":256}`)},
		{level: "INFO", component: "router", msg: "request handled", source: "router.go:112", attrs: j(`{"method":"DELETE","path":"/api/v1/sessions","status":204,"duration_ms":8,"bytes":0}`)},
		{level: "INFO", component: "health", msg: "health check passed", source: "health.go:15", attrs: j(`{"checks":{"db":"ok","redis":"ok","nats":"ok"}}`)},
		{level: "INFO", component: "server", msg: "server started", source: "server.go:34", attrs: j(`{"addr":":8080","version":"v1.2.3"}`)},
		// DEBUG
		{level: "DEBUG", component: "router", msg: "request received", source: "router.go:45", attrs: j(`{"method":"GET","path":"/api/v1/orders","remote_addr":"10.0.5.20","request_id":"req_abc123"}`)},
		{level: "DEBUG", component: "router", msg: "request received", source: "router.go:45", attrs: j(`{"method":"POST","path":"/api/v1/payments","remote_addr":"10.0.5.21","request_id":"req_def456"}`)},
		{level: "DEBUG", component: "ratelimit", msg: "rate limit check", source: "ratelimit.go:28", attrs: j(`{"remote_addr":"10.0.5.20","remaining":97,"limit":100,"window":"1m"}`)},
		// WARN
		{level: "WARN", component: "router", msg: "slow request", source: "router.go:118", attrs: j(`{"method":"GET","path":"/api/v1/orders","status":200,"duration_ms":2340,"threshold_ms":1000}`)},
		{level: "WARN", component: "ratelimit", msg: "rate limit exceeded", source: "ratelimit.go:52", attrs: j(`{"remote_addr":"198.51.100.30","path":"/api/v1/auth/login","limit":100,"window":"1m"}`)},
		{level: "WARN", component: "health", msg: "health check degraded", source: "health.go:28", attrs: j(`{"checks":{"db":"ok","redis":"timeout","nats":"ok"}}`)},
		{level: "WARN", component: "router", msg: "request body too large", source: "router.go:78", attrs: j(`{"method":"POST","path":"/api/v1/uploads","content_length":52428800,"max_bytes":10485760}`)},
		// ERROR
		{level: "ERROR", component: "router", msg: "upstream service unavailable", source: "router.go:125", attrs: j(`{"method":"POST","path":"/api/v1/payments","upstream":"payment-service","error":"dial tcp 10.0.5.12:8081: connection refused"}`)},
		{level: "ERROR", component: "router", msg: "request panicked", source: "router.go:130", attrs: j(`{"method":"GET","path":"/api/v1/orders/bad","error":"runtime error: index out of range [5] with length 3","stack":"goroutine 42 [running]:\nmain.handleOrder(...)"}`)},
		{level: "ERROR", component: "server", msg: "TLS handshake error", source: "server.go:55", attrs: j(`{"remote_addr":"198.51.100.40","error":"tls: client offered only unsupported versions"}`)},
		// FATAL
		{level: "FATAL", component: "server", msg: "failed to bind listen address, shutting down", source: "server.go:22", attrs: j(`{"addr":":8080","error":"bind: address already in use"}`)},
	},
}

// --- inventory-service (weight 5) ---

var inventoryService = serviceProfile{
	weight:  5,
	service: "inventory-service",
	msgs: []applogMsg{
		// INFO
		{level: "INFO", component: "stock", msg: "stock level updated", source: "stock.go:45", attrs: j(`{"sku":"SKU-1234","warehouse":"wh-east-01","previous":150,"current":147}`)},
		{level: "INFO", component: "stock", msg: "stock level updated", source: "stock.go:45", attrs: j(`{"sku":"SKU-5678","warehouse":"wh-west-02","previous":30,"current":80,"reason":"restock"}`)},
		{level: "INFO", component: "stock", msg: "stock reservation created", source: "stock.go:78", attrs: j(`{"sku":"SKU-1234","order_id":"ord_9f2a","quantity":3,"reserved_until":"2026-02-04T15:00:00Z"}`)},
		{level: "INFO", component: "sync", msg: "inventory sync completed", source: "sync.go:55", attrs: j(`{"source":"erp","items_synced":1250,"duration_ms":4500}`)},
		// DEBUG
		{level: "DEBUG", component: "stock", msg: "checking stock availability", source: "stock.go:30", attrs: j(`{"sku":"SKU-1234","warehouse":"wh-east-01","available":150}`)},
		{level: "DEBUG", component: "sync", msg: "starting inventory sync", source: "sync.go:22", attrs: j(`{"source":"erp","last_sync":"2026-02-04T12:00:00Z"}`)},
		// WARN
		{level: "WARN", component: "stock", msg: "stock level low", source: "stock.go:52", attrs: j(`{"sku":"SKU-9012","warehouse":"wh-east-01","current":5,"reorder_point":10}`)},
		{level: "WARN", component: "stock", msg: "stock reservation expired", source: "stock.go:92", attrs: j(`{"sku":"SKU-1234","order_id":"ord_exp1","reserved_quantity":2}`)},
		// ERROR
		{level: "ERROR", component: "stock", msg: "stock level went negative", source: "stock.go:58", attrs: j(`{"sku":"SKU-3456","warehouse":"wh-west-02","current":-2,"error":"oversold"}`)},
		{level: "ERROR", component: "sync", msg: "inventory sync failed", source: "sync.go:62", attrs: j(`{"error":"erp: connection timeout after 30s","source":"erp","retry_count":3}`)},
		// FATAL
		{level: "FATAL", component: "stock", msg: "data corruption detected, shutting down", source: "stock.go:18", attrs: j(`{"error":"checksum mismatch in stock ledger","table":"stock_levels","rows_affected":42}`)},
	},
}

// j is a shorthand for creating json.RawMessage from a string literal.
func j(s string) json.RawMessage {
	return json.RawMessage(s)
}
