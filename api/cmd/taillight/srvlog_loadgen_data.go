package main

// srvlogMsg holds a structured syslog message with its message ID for server logs.
type srvlogMsg struct {
	msgid   string
	message string
}

// serverProfile groups all server-specific data for coherent log generation.
type serverProfile struct {
	weight     int
	hostnames  []string
	ipPrefix   string
	programs   []string
	messages   []srvlogMsg
	facilities []int
}

// srvlogSeverityWeights produces realistic severity distributions for servers.
var srvlogSeverityWeights = []severityWeight{
	{6, 500}, // info
	{5, 270}, // notice
	{4, 100}, // warning
	{7, 80},  // debug
	{3, 30},  // err
	{2, 10},  // crit
	{1, 7},   // alert
	{0, 3},   // emerg
}

var serverProfiles = []serverProfile{
	linuxProfile,
	nginxProfile,
	postgresProfile,
	dockerProfile,
	systemdProfile,
}

// --- Linux system (weight 30) ---

var linuxProfile = serverProfile{
	weight:   30,
	ipPrefix: "10.10.1",
	hostnames: []string{
		"web-01.srv", "web-02.srv", "web-03.srv",
		"app-01.srv", "app-02.srv",
		"db-01.srv", "db-02.srv",
		"cache-01.srv",
		"worker-01.srv", "worker-02.srv",
	},
	programs: []string{
		"sshd", "sudo", "cron", "kernel", "systemd-logind",
		"useradd", "passwd", "su",
	},
	facilities: []int{0, 1, 4, 10}, // kern, user, auth, authpriv
	messages: []srvlogMsg{
		// SSH
		{"", "Accepted publickey for deploy from 10.10.0.5 port 52341 ssh2: RSA SHA256:abc123"},
		{"", "Accepted publickey for admin from 10.10.0.10 port 41222 ssh2: ED25519 SHA256:xyz789"},
		{"", "Failed password for invalid user root from 203.0.113.45 port 33721 ssh2"},
		{"", "Failed password for admin from 198.51.100.22 port 44123 ssh2"},
		{"", "Disconnected from user deploy 10.10.0.5 port 52341"},
		{"", "Connection closed by authenticating user admin 10.10.0.10 port 41222 [preauth]"},
		{"", "Invalid user oracle from 203.0.113.99 port 55443"},
		{"", "pam_unix(sshd:session): session opened for user deploy(uid=1001) by (uid=0)"},
		{"", "pam_unix(sshd:session): session closed for user deploy"},
		// Sudo
		{"", "deploy : TTY=pts/0 ; PWD=/home/deploy ; USER=root ; COMMAND=/usr/bin/systemctl restart nginx"},
		{"", "admin : TTY=pts/1 ; PWD=/root ; USER=root ; COMMAND=/usr/bin/apt update"},
		{"", "deploy : TTY=pts/0 ; PWD=/home/deploy ; USER=root ; COMMAND=/usr/bin/journalctl -u app"},
		{"", "admin : command not allowed ; TTY=pts/1 ; PWD=/tmp ; USER=root ; COMMAND=/bin/rm -rf /"},
		// Cron
		{"CRON", "CMD (/usr/local/bin/backup.sh >> /var/log/backup.log 2>&1)"},
		{"CRON", "CMD (/usr/bin/certbot renew --quiet)"},
		{"CRON", "CMD (run-parts /etc/cron.daily)"},
		{"CRON", "CMD (/usr/local/bin/health-check.sh)"},
		// Kernel
		{"", "Out of memory: Killed process 12345 (java) total-vm:4096000kB, anon-rss:3200000kB"},
		{"", "TCP: request_sock_TCP: Possible SYN flooding on port 443. Sending cookies."},
		{"", "nf_conntrack: table full, dropping packet"},
		{"", "EXT4-fs (sda1): mounted filesystem with ordered data mode"},
		{"", "[UFW BLOCK] IN=eth0 OUT= MAC=aa:bb:cc:dd:ee:ff SRC=203.0.113.50 DST=10.10.1.5 PROTO=TCP DPT=22"},
		{"", "device eth0 entered promiscuous mode"},
		{"", "EDAC MC0: 1 CE memory read error on CPU_SrcID#0_Ha#0_Chan#0_DIMM#0"},
		// PAM / Login
		{"", "pam_unix(su:session): session opened for user postgres(uid=111) by admin(uid=1000)"},
		{"", "pam_unix(systemd-user:session): session opened for user deploy(uid=1001) by (uid=0)"},
	},
}

// --- Nginx (weight 25) ---

var nginxProfile = serverProfile{
	weight:   25,
	ipPrefix: "10.10.1",
	hostnames: []string{
		"web-01.srv", "web-02.srv", "web-03.srv",
		"lb-01.srv", "lb-02.srv",
	},
	programs:   []string{"nginx"},
	facilities: []int{1, 3}, // user, daemon
	messages: []srvlogMsg{
		// Access-like via syslog
		{"", "10.10.0.50 - - GET /api/v1/users HTTP/1.1 200 1234 0.012"},
		{"", "10.10.0.51 - - POST /api/v1/auth/login HTTP/1.1 401 89 0.003"},
		{"", "10.10.0.52 - - GET /health HTTP/1.1 200 2 0.001"},
		{"", "10.10.0.53 - - GET /static/app.js HTTP/1.1 304 0 0.000"},
		// Errors
		{"", "connect() failed (111: Connection refused) while connecting to upstream, client: 10.10.0.50, upstream: http://127.0.0.1:8080/api/"},
		{"", "upstream timed out (110: Connection timed out) while reading response header from upstream, client: 10.10.0.51"},
		{"", "open() \"/var/www/html/favicon.ico\" failed (2: No such file or directory), client: 10.10.0.52"},
		{"", "client intended to send too large body: 52428800 bytes, client: 203.0.113.10"},
		{"", "SSL_do_handshake() failed (SSL: error:14094412:SSL routines:ssl3_read_bytes:sslv3 alert bad certificate)"},
		{"", "upstream prematurely closed connection while reading response header from upstream"},
		// Lifecycle
		{"", "signal process started"},
		{"", "worker process 12345 exited with code 0"},
		{"", "start worker processes"},
		{"", "graceful shutdown, saving connections"},
		// Rate limiting
		{"", "limiting requests, excess: 20.456 by zone \"api_limit\", client: 203.0.113.100"},
		{"", "limiting connections by zone \"conn_limit\", client: 203.0.113.101"},
	},
}

// --- PostgreSQL (weight 20) ---

var postgresProfile = serverProfile{
	weight:   20,
	ipPrefix: "10.10.2",
	hostnames: []string{
		"db-01.srv", "db-02.srv",
		"db-replica-01.srv",
	},
	programs:   []string{"postgres", "pg_dump", "pg_basebackup"},
	facilities: []int{1, 3}, // user, daemon
	messages: []srvlogMsg{
		// Connections
		{"", "connection received: host=10.10.1.5 port=52341"},
		{"", "connection authorized: user=app database=production"},
		{"", "disconnection: session time: 0:05:12.345 user=app database=production host=10.10.1.5 port=52341"},
		{"", "too many connections for role \"app\""},
		// Slow queries
		{"", "duration: 5234.567 ms  statement: SELECT * FROM orders WHERE created_at > '2024-01-01'"},
		{"", "duration: 12045.123 ms  statement: UPDATE inventory SET quantity = quantity - 1 WHERE product_id = 42"},
		// Errors
		{"", "ERROR:  duplicate key value violates unique constraint \"users_email_key\""},
		{"", "ERROR:  deadlock detected"},
		{"", "DETAIL:  Process 1234 waits for ShareLock on transaction 5678; blocked by process 9012"},
		{"", "ERROR:  canceling statement due to statement timeout"},
		{"", "FATAL:  password authentication failed for user \"admin\""},
		{"", "ERROR:  relation \"nonexistent_table\" does not exist at character 15"},
		// Replication
		{"", "redo starts at 0/5000028"},
		{"", "consistent recovery state reached at 0/5000100"},
		{"", "started streaming WAL from primary at 0/6000000 on timeline 1"},
		{"", "replication terminated by primary server"},
		// Maintenance
		{"", "checkpoint starting: time"},
		{"", "checkpoint complete: wrote 1234 buffers (7.5%); 0 WAL file(s) added"},
		{"", "automatic vacuum of table \"production.public.events\": index scans: 1, pages: 0 removed"},
		{"", "automatic analyze of table \"production.public.orders\""},
	},
}

// --- Docker (weight 15) ---

var dockerProfile = serverProfile{
	weight:   15,
	ipPrefix: "10.10.1",
	hostnames: []string{
		"docker-01.srv", "docker-02.srv", "docker-03.srv",
		"k8s-node-01.srv", "k8s-node-02.srv",
	},
	programs:   []string{"dockerd", "containerd", "kubelet"},
	facilities: []int{1, 3}, // user, daemon
	messages: []srvlogMsg{
		// Container lifecycle
		{"", "Container 8a3f2b started: name=api-production image=api:v2.3.1"},
		{"", "Container c7d1e9 stopped: name=api-production exitCode=0"},
		{"", "Container f4b8a2 died: name=worker-01 exitCode=137 (OOMKilled)"},
		{"", "Container 2e9c1d health check failed: name=api-staging unhealthy"},
		// Image operations
		{"", "Pulling image: registry.internal/api:v2.3.2"},
		{"", "Pull complete: registry.internal/api:v2.3.2"},
		{"", "Error pulling image: registry.internal/api:v2.3.2 - connection refused"},
		// Resource issues
		{"", "Container a1b2c3 exceeded memory limit: name=java-app limit=2g usage=2.1g"},
		{"", "No space left on device: cannot create layer"},
		{"", "Container network connection timeout: name=api-production network=bridge"},
		// Kubelet
		{"", "Pod sandbox changed, it will be killed and re-created: pod=api-7f8b9c-x2k4z"},
		{"", "Container runtime is not ready: network plugin is not ready: cni config uninitialized"},
		{"", "Successfully pulled image \"registry.internal/api:v2.3.2\" in 3.2s"},
		{"", "Liveness probe failed: HTTP probe failed with statuscode: 503"},
		{"", "Back-off restarting failed container api in pod api-7f8b9c-x2k4z"},
	},
}

// --- Systemd (weight 10) ---

var systemdProfile = serverProfile{
	weight:   10,
	ipPrefix: "10.10.1",
	hostnames: []string{
		"web-01.srv", "web-02.srv", "app-01.srv",
		"db-01.srv", "worker-01.srv",
	},
	programs:   []string{"systemd", "systemd-resolved", "systemd-timesyncd", "systemd-networkd"},
	facilities: []int{1, 3}, // user, daemon
	messages: []srvlogMsg{
		// Service management
		{"", "Started nginx.service - A high performance web server and reverse proxy server."},
		{"", "Stopped nginx.service - A high performance web server and reverse proxy server."},
		{"", "Starting postgresql@14-main.service - PostgreSQL Cluster 14-main..."},
		{"", "app.service: Main process exited, code=exited, status=1/FAILURE"},
		{"", "app.service: Failed with result 'exit-code'."},
		{"", "app.service: Scheduled restart job, restart counter is at 5."},
		{"", "app.service: Start request repeated too quickly. Refusing to start."},
		// System
		{"", "Started Daily apt download activities."},
		{"", "Finished Daily apt download activities."},
		{"", "Started Run logrotate."},
		{"", "Reached target Multi-User System."},
		// Network / DNS
		{"", "Using degraded feature set (UDP) for DNS server 10.10.0.2."},
		{"", "Clock synchronized to time server 10.10.0.1 (ntp.internal)."},
		{"", "eth0: DHCPv4 address 10.10.1.5/24 via 10.10.1.1"},
		// OOM / Resources
		{"", "systemd-oomd: Killed /system.slice/app.service due to memory pressure"},
		{"", "system.slice: memory usage 14.2G is above limit 16.0G, sending SIGKILL"},
	},
}
