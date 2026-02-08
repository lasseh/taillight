package main

// syslogMsg holds a structured syslog message with its message ID.
type syslogMsg struct {
	msgid   string
	message string
}

// vendorProfile groups all vendor-specific data for coherent log generation.
type vendorProfile struct {
	weight     int      // relative probability weight
	hostnames  []string // vendor-specific hostnames
	ipPrefix   string   // e.g. "10.0.1" → generates 10.0.1.x
	programs   []string // vendor-specific program names
	messages   []syslogMsg
	facilities []int // common syslog facility codes for this vendor
}

// severityWeight maps a severity level to its relative probability.
type severityWeight struct {
	severity int
	weight   int
}

// severityWeights produces realistic severity distributions.
// info(6): 50%, notice(5): 27%, warning(4): 10%, debug(7): 8%,
// err(3): 3%, crit(2): 1%, alert(1): 0.7%, emerg(0): 0.3%.
var severityWeights = []severityWeight{
	{6, 500}, // info
	{5, 270}, // notice
	{4, 100}, // warning
	{7, 80},  // debug
	{3, 30},  // err
	{2, 10},  // crit
	{1, 7},   // alert
	{0, 3},   // emerg
}

var vendors = []vendorProfile{
	juniperProfile,
	ciscoProfile,
	aristaProfile,
	noiseProfile,
}

// --- Noise: messages that rsyslog filters will drop (weight 10) ---

var noiseProfile = vendorProfile{
	weight:   10,
	ipPrefix: "10.0.1",
	hostnames: []string{
		"core-rtr01.lab", "core-rtr02.lab", "edge-rtr01.lab",
		"dist-sw01.lab", "cat-sw01.lab",
	},
	programs: []string{
		"cron", "ntpd", "mib2d", "sshd",
	},
	facilities: []int{1, 9, 15}, // user, cron, local0
	messages: []syslogMsg{
		{"CRON", "CMD (/usr/sbin/ntpdate -s time.nist.gov)"},
		{"CRON", "CMD (/usr/lib/sa/sa1 1 1)"},
		{"CRON", "CMD (run-parts /etc/cron.hourly)"},
		{"", "NTP peer 10.0.0.254 reachable, offset -0.003ms"},
		{"", "NTP synchronized to 10.0.0.254, stratum 2"},
		{"", "SNMP get response: sysUpTime.0 = 1234567"},
		{"", "Accepted publickey for admin from 10.0.0.5 port 22 ssh2"},
		{"", "session opened for user admin by (uid=0)"},
		{"", "session closed for user admin"},
	},
}

// --- Juniper (weight 60) ---

var juniperProfile = vendorProfile{
	weight:   60,
	ipPrefix: "10.0.1",
	hostnames: []string{
		"core-rtr01.lab", "core-rtr02.lab", "core-rtr03.lab",
		"edge-rtr01.lab", "edge-rtr02.lab",
		"dist-sw01.lab", "dist-sw02.lab",
		"fw-srx01.lab", "fw-srx02.lab",
		"spine-qfx01.lab", "spine-qfx02.lab",
		"leaf-qfx01.lab", "leaf-qfx02.lab", "leaf-qfx03.lab",
		"lab-mx01.lab",
	},
	programs: []string{
		"rpd", "mgd", "chassisd", "alarmd", "pfed",
		"snmpd", "mib2d", "dcd", "eventd", "craftd",
		"lacpd", "l2ald", "dfwd", "appidd", "license-check",
	},
	facilities: []int{0, 1, 3, 4, 5, 23}, // kern, user, daemon, auth, syslog, local7
	messages: []syslogMsg{
		// BGP
		{"RPD_BGP_NEIGHBOR_STATE_CHANGED", "BGP peer 10.0.0.1 (AS 65001) state changed from Established to Idle"},
		{"RPD_BGP_NEIGHBOR_STATE_CHANGED", "BGP peer 10.0.0.2 (AS 65002) state changed from Idle to OpenSent"},
		{"RPD_BGP_NEIGHBOR_STATE_CHANGED", "BGP peer 172.16.0.1 (AS 65100) changed state from OpenConfirm to Established"},
		{"RPD_BGP_NEIGHBOR_STATE_CHANGED", "BGP peer 10.0.0.3 (AS 65003) state changed from Active to Established"},
		{"RPD_BGP_NEIGHBOR_STATE_CHANGED", "BGP peer 192.168.1.1 (AS 65010) state changed from Established to Active"},
		{"", "BGP session with 10.0.0.4 flapping: 5 transitions in 60 seconds"},
		{"", "BGP received max-prefix limit (10000) exceeded from peer 10.0.0.5 (AS 65005)"},
		// OSPF
		{"", "OSPF adjacency on ae0.0 changed from Full to Down"},
		{"", "OSPF adjacency on ae1.0 changed from Init to Full"},
		{"", "OSPF interface ge-0/0/0.0 entered state DR"},
		{"", "OSPF area 0.0.0.0 SPF calculation completed in 12ms"},
		// ISIS
		{"RPD_ISIS_ADJCHANGE", "IS-IS change 0001 interface ae1.0 level 2 adjacency DOWN"},
		{"RPD_ISIS_ADJCHANGE", "IS-IS change 0002 interface ae2.0 level 1 adjacency UP"},
		{"RPD_ISIS_ADJCHANGE", "IS-IS change 0003 interface ge-0/0/1.0 level 2 adjacency UP"},
		// MPLS / LDP / RSVP
		{"RPD_MPLS_LSP_CHANGE", "MPLS LSP to-core02 changed state from Up to Down"},
		{"RPD_MPLS_LSP_CHANGE", "MPLS LSP to-edge01 changed state from Down to Up"},
		{"RPD_MPLS_LSP_CHANGE", "MPLS LSP backup-core02 changed state from Standby to Active"},
		{"RPD_LDP_NBRDOWN", "LDP neighbor 10.0.0.2 session closed: hold timer expired"},
		{"RPD_LDP_NBRUP", "LDP neighbor 10.0.0.3 session established"},
		{"RPD_RSVP_RESV_TEAR", "RSVP reservation teardown for LSP to-core01 path primary"},
		// BFD
		{"BFD_STATE_CHANGE", "BFD session 10.0.0.2 state changed from Up to Down"},
		{"BFD_STATE_CHANGE", "BFD session 10.0.0.3 state changed from Down to Up"},
		{"BFD_STATE_CHANGE", "BFD session 172.16.0.5 state changed from Up to AdminDown"},
		// Chassis
		{"CHASSISD_SNMP_TRAP7", "SNMP trap generated: FRU power off (FPC 3)"},
		{"CHASSISD_FPC_OFFLINE", "fpc 2 taken offline"},
		{"CHASSISD_FPC_ONLINE", "fpc 0 online"},
		{"CHASSISD_TEMP_CHECK", "temperature check: FPC 0 exhaust temp 45C"},
		{"CHASSISD_TEMP_CHECK", "temperature check: FPC 1 exhaust temp 62C (high threshold 65C)"},
		{"CHASSISD_PSU_VOLTAGE", "PSU 0 input voltage 228V within normal range"},
		{"CHASSISD_PSU_FAILURE", "PSU 1 failure detected: output voltage out of range"},
		{"CHASSISD_CB_READ", "RE0 mastership retained"},
		// Alarms
		{"ALARM_SET", "color=RED class=CHASSIS reason=FPC 3 Major Errors"},
		{"ALARM_SET", "color=YELLOW class=CHASSIS reason=PSU 1 input failure"},
		{"ALARM_CLEARED", "color=YELLOW class=CHASSIS reason=FPC 2 Major Errors cleared"},
		// Auth / UI / Commit
		{"UI_AUTH_EVENT", "Authenticated user root from host 10.0.0.5"},
		{"UI_AUTH_EVENT", "Authenticated user admin from host 10.0.0.10"},
		{"UI_LOGIN_EVENT", "User admin logged in from 10.0.0.5 via ssh"},
		{"UI_LOGIN_EVENT", "User noc logged in from 10.0.0.20 via console"},
		{"UI_LOGOUT_EVENT", "User admin logged out from 10.0.0.5"},
		{"UI_COMMIT_COMPLETED", "commit complete (by user admin)"},
		{"UI_COMMIT_COMPLETED", "commit complete (by user noc)"},
		{"UI_COMMIT_NOT_CONFIRMED", "commit not confirmed; rolling back"},
		// Firewall
		{"PFE_FW_SYSLOG_ETH", "FW: xe-0/0/1.0 D 192.168.1.5 192.168.2.10 TCP 443 12345"},
		{"PFE_FW_SYSLOG_ETH", "FW: xe-0/0/2.0 A 10.1.1.5 10.2.2.10 UDP 53 44821"},
		{"PFE_FW_SYSLOG_ETH", "FW: ae0.0 D 172.16.5.1 172.16.10.1 ICMP"},
		// SNMP
		{"SNMPD_AUTH_FAILURE", "nap_err_snmpd_auth_fail: SNMP authentication failure from 10.0.0.99"},
		{"SNMPD_TRAP_COLD_START", "cold start trap sent"},
		// LACP
		{"LACPD_TIMEOUT", "lacp current while timer expired on interface ae0"},
		{"LACPD_TIMEOUT", "lacp current while timer expired on interface ae3"},
		{"LACPD_INTF_DOWN", "lacp interface ae1 member link xe-0/0/0 detached"},
		// ARP
		{"KERN_ARP_ADDR_CHANGE", "arp info overwritten for 10.0.0.1 from 00:11:22:33:44:55 to aa:bb:cc:dd:ee:ff"},
		{"KERN_ARP_ADDR_CHANGE", "arp info overwritten for 10.0.0.10 from 00:aa:bb:cc:dd:01 to 00:aa:bb:cc:dd:02"},
		// L2ALD
		{"L2ALD_MAC_LIMIT", "MAC address limit reached on interface ae0.100: limit 1024"},
		{"L2ALD_MAC_MOVE", "MAC move detected: 00:11:22:33:44:55 moved from ae0 to ae1"},
		// License
		{"LICENSE_EXPIRED", "license for feature idp-sig expired"},
		{"LICENSE_ABOUT_TO_EXPIRE", "license for feature bgp will expire in 7 days"},
		// Misc daemon
		{"DCD_MALLOC_FAILED_INIT", "malloc failed during initialization"},
		{"MIB2D_IFD_IFINDEX_FAILURE", "failed to get ifindex for interface ge-0/0/5"},
		{"EVENTD_MEMORY_USAGE_OK", "Memory utilization is within normal limits"},
	},
}

// --- Cisco (weight 25) ---

var ciscoProfile = vendorProfile{
	weight:   25,
	ipPrefix: "10.0.2",
	hostnames: []string{
		"core-asr01.lab", "core-asr02.lab",
		"wan-isr01.lab", "wan-isr02.lab",
		"cat-sw01.lab", "cat-sw02.lab", "cat-sw03.lab",
		"dc-n9k-spine01.lab", "dc-n9k-spine02.lab",
		"dc-n9k-leaf01.lab", "dc-n9k-leaf02.lab",
		"wlc-01.lab",
	},
	programs: []string{
		"OSPF", "BGP", "STP", "LINEPROTO", "SYS",
		"ILPOWER", "PLATFORM", "EIGRP", "HSRP", "LINK",
		"CDP", "ETHPORT",
	},
	facilities: []int{0, 1, 3, 4, 23}, // kern, user, daemon, auth, local7
	messages: []syslogMsg{
		// BGP
		{"BGP-5-ADJCHANGE", "neighbor 10.1.0.1 Up"},
		{"BGP-5-ADJCHANGE", "neighbor 10.1.0.2 Down BGP Notification sent: hold time expired"},
		{"BGP-3-NOTIFICATION", "received from neighbor 10.1.0.3 4/0 (hold time expired)"},
		{"BGP-5-ADJCHANGE", "neighbor 172.16.1.1 Up (AS 65200)"},
		// OSPF
		{"OSPF-5-ADJCHG", "Process 1, Nbr 10.1.0.10 on GigabitEthernet0/1 from FULL to DOWN, Neighbor Down"},
		{"OSPF-5-ADJCHG", "Process 1, Nbr 10.1.0.11 on GigabitEthernet0/2 from LOADING to FULL, Loading Done"},
		{"OSPF-4-FLOOD_WAR", "Process 1 re-originating LSA type 1, LSID 10.1.0.1, adv-rtr 10.1.0.1, seq 80000010"},
		// EIGRP
		{"EIGRP-5-NBRCHANGE", "IP-EIGRP(0) 100: Neighbor 10.1.0.20 (GigabitEthernet0/0) is up"},
		{"EIGRP-5-NBRCHANGE", "IP-EIGRP(0) 100: Neighbor 10.1.0.21 (GigabitEthernet0/1) is down: holding time expired"},
		// STP
		{"SPANTREE-2-ROOTGUARD_BLOCK", "Root guard blocking port GigabitEthernet1/0/24 on VLAN0100"},
		{"SPANTREE-2-LOOPGUARD_BLOCK", "Loop guard blocking port GigabitEthernet1/0/23 on VLAN0200"},
		{"SPANTREE-5-TOPOTCHANGE", "Topology change detected on GigabitEthernet1/0/1 VLAN0010"},
		{"STP-2-CIST_ROOT_CHANGE", "CIST root changed to bridge priority 4096 address 00:aa:bb:cc:00:01"},
		// Interface
		{"LINK-3-UPDOWN", "Interface GigabitEthernet0/1, changed state to down"},
		{"LINK-3-UPDOWN", "Interface GigabitEthernet0/1, changed state to up"},
		{"LINK-3-UPDOWN", "Interface Port-channel1, changed state to down"},
		{"LINEPROTO-5-UPDOWN", "Line protocol on Interface GigabitEthernet0/2, changed state to up"},
		{"LINEPROTO-5-UPDOWN", "Line protocol on Interface TenGigabitEthernet1/1, changed state to down"},
		// HSRP
		{"HSRP-5-STATECHANGE", "GigabitEthernet0/1 Grp 1 state Standby -> Active"},
		{"HSRP-5-STATECHANGE", "GigabitEthernet0/1 Grp 1 state Active -> Speak"},
		// SYS
		{"SYS-5-CONFIG_I", "Configured from console by admin on vty0 (10.1.0.50)"},
		{"SYS-5-CONFIG_I", "Configured from console by noc on vty1 (10.1.0.51)"},
		{"SYS-5-RELOAD", "Reload requested by admin. Reload Reason: configuration change"},
		{"SYS-5-RESTART", "System restarted -- Cisco IOS Software, Version 15.7(3)M"},
		// CDP
		{"CDP-4-DUPLEX_MISMATCH", "duplex mismatch discovered on GigabitEthernet1/0/5 (not half duplex) with cat-sw02.lab GigabitEthernet1/0/5 (half duplex)"},
		{"CDP-4-NATIVE_VLAN_MISMATCH", "Native VLAN mismatch on GigabitEthernet1/0/10 (1) with cat-sw03.lab (100)"},
		// PoE
		{"ILPOWER-5-POWER_GRANTED", "Interface Gi1/0/15: Power granted"},
		{"ILPOWER-7-DETECT", "Interface Gi1/0/16: Power Device detected: IEEE PD"},
		// NX-OS vPC
		{"VPC-2-PEER_KEEP_ALIVE_RECV_FAIL", "vPC peer keep-alive receive failed"},
		{"VPC-5-PEER_KEEP_ALIVE_RECV_SUCCESS", "vPC peer keep-alive receive success from peer 10.1.0.100"},
		{"ETHPORT-5-IF_UP", "Interface Ethernet1/1 is up"},
		{"ETHPORT-5-IF_DOWN_LINK_FAILURE", "Interface Ethernet1/2 is down (Link failure)"},
		// ACL
		{"SEC-6-IPACCESSLOGP", "list OUTSIDE-IN denied tcp 198.51.100.5(44123) -> 10.1.0.5(22), 1 packet"},
		{"SEC-6-IPACCESSLOGP", "list INSIDE-OUT permitted udp 10.1.0.10(53421) -> 8.8.8.8(53), 5 packets"},
		{"SEC-6-IPACCESSLOGP", "list MGMT-ACL denied tcp 192.168.1.100(54321) -> 10.1.0.1(23), 1 packet"},
	},
}

// --- Arista (weight 15) ---

var aristaProfile = vendorProfile{
	weight:   15,
	ipPrefix: "10.0.3",
	hostnames: []string{
		"spine-eos01.lab", "spine-eos02.lab", "spine-eos03.lab",
		"leaf-eos01.lab", "leaf-eos02.lab", "leaf-eos03.lab",
		"bleaf-eos01.lab", "bleaf-eos02.lab",
		"mgmt-eos01.lab",
		"tor-eos01.lab",
	},
	programs: []string{
		"Ebra", "Stp", "Bgp", "Intf", "Mlag",
		"ProcMgr", "Lldp", "Acl", "Rib", "ConfigAgent",
	},
	facilities: []int{0, 1, 3, 4, 5, 23}, // kern, user, daemon, auth, syslog, local7
	messages: []syslogMsg{
		// BGP
		{"BGP-5-ADJCHANGE", "peer 10.2.0.1 (AS 65301) old state Established event Stop new state Idle"},
		{"BGP-5-ADJCHANGE", "peer 10.2.0.2 (AS 65302) old state Active event Start new state Connect"},
		{"BGP-5-ADJCHANGE", "peer 10.2.0.3 (AS 65303) old state OpenConfirm event RecvOpen new state Established"},
		{"BGP-3-NOTIFICATION", "peer 10.2.0.4 (AS 65304) sent notification: cease/admin-shutdown"},
		// STP
		{"STP-6-STABLE_CHANGE", "Spanning tree became stable"},
		{"STP-6-INTERFACE_ADD", "Interface Ethernet1 added to VLAN 100 in instance MST0"},
		{"STP-2-BLOCK_BPDUGUARD", "Received BPDU on BPDU guard enabled port Ethernet47, putting in err-disabled state"},
		// Interface
		{"INTF-5-CHANGED", "Interface Ethernet1 changed state to up"},
		{"INTF-5-CHANGED", "Interface Ethernet2 changed state to down"},
		{"INTF-5-CHANGED", "Interface Port-Channel1 changed state to up"},
		{"INTF-3-IF_DOWN_ERROR_DISABLED", "Interface Ethernet48, changed state to errdisabled"},
		// MLAG
		{"MLAG-4-DISABLED_ON_DUAL_PRIMARY", "Dual-primary detected: MLAG disabled"},
		{"MLAG-6-PEER_UP", "MLAG peer 10.2.0.100 is now up"},
		{"MLAG-6-PEER_DOWN", "MLAG peer 10.2.0.100 is now down: heartbeat timeout"},
		{"MLAG-5-PORTS_ERRDISABLED", "MLAG port Ethernet3 is error-disabled: no peer link"},
		// LLDP
		{"LLDP-5-NEIGHBOR_NEW", "New LLDP neighbor spine-eos02.lab on Ethernet49/1"},
		{"LLDP-5-NEIGHBOR_REMOVED", "LLDP neighbor spine-eos01.lab removed from Ethernet50/1"},
		// ProcMgr
		{"PROCMGR-6-PROCESS_RESTART", "Restarting process Stp (PID 1234) [restart count 1]"},
		{"PROCMGR-7-WORKER_WARMSTART", "ProcMgr worker warm start complete"},
		// SYS / Config
		{"SYS-5-CONFIG_STARTUP", "Startup config saved by admin from session 10.2.0.50"},
		{"SYS-5-CONFIG_SESSION_ENTERED", "User admin entered config session via cli"},
		// ACL
		{"ACL-6-IPACCESS", "list MGMT-IN denied tcp 198.51.100.10(55123) 10.2.0.1(22)"},
		{"ACL-6-IPACCESS", "list LEAF-OUT permitted icmp 10.2.0.5 10.2.0.1"},
		// Environment
		{"ENV-4-FAN_SPEED_HIGH", "Fan tray 1 speed increased to 80% due to high temperature"},
		{"ENV-3-PSU_FAILURE", "Power supply 2 has failed"},
		{"ENV-6-PSU_OK", "Power supply 1 is operating normally"},
		// RIB
		{"RIB-6-ROUTE_LIMIT_WARN", "IPv4 unicast route count 240000 approaching limit 256000"},
	},
}
