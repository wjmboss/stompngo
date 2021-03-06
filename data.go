//
// Copyright © 2011-2017 Guy M. Allard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed, an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package stompngo

import (
	"bufio"
	"log"
	"net"
	"sync"
	"time"
)

const (

	// Client generated commands.
	CONNECT     = "CONNECT"
	STOMP       = "STOMP"
	DISCONNECT  = "DISCONNECT"
	SEND        = "SEND"
	SUBSCRIBE   = "SUBSCRIBE"
	UNSUBSCRIBE = "UNSUBSCRIBE"
	ACK         = "ACK"
	NACK        = "NACK"
	BEGIN       = "BEGIN"
	COMMIT      = "COMMIT"
	ABORT       = "ABORT"

	// Server generated commands.
	CONNECTED = "CONNECTED"
	MESSAGE   = "MESSAGE"
	RECEIPT   = "RECEIPT"
	ERROR     = "ERROR"

	// Supported STOMP protocol definitions.
	SPL_10 = "1.0"
	SPL_11 = "1.1"
	SPL_12 = "1.2"
)

/*
	What this package currently supports.
*/
var supported = []string{SPL_10, SPL_11, SPL_12}

/*
	Headers definition, a slice of string.

	STOMP headers are key and value pairs.  See the specification for more
	information about STOMP frame headers.

	Key values are found at even numbered indices.  Values
	are found at odd numbered indices.  Headers are validated for an even
	number of slice elements.
*/
type Headers []string

/*
	Message is a STOMP Message, consisting of: a STOMP command; a set of STOMP
	Headers; and a message body(payload), which is possibly empty.
*/
type Message struct {
	Command string
	Headers Headers
	Body    []uint8
}

/*
	Frame is an alternate name for a Message.
*/
type Frame Message

/*
	MessageData passed to the client, containing: the Message; and an Error
	value which is possibly nil.

	Note that this has no relevance on whether a MessageData.Message.Command
	value contains an "ERROR" generated by the broker.
*/
type MessageData struct {
	Message Message
	Error   error
}

/*
	This is outbound on the wire.
*/
type wiredata struct {
	frame   Frame
	errchan chan error
}

/*
	Stomper is an interface that models STOMP specification commands.
*/
type Stomper interface {
	Abort(h Headers) error
	Ack(headers Headers) error
	Begin(h Headers) error
	Commit(h Headers) error
	Disconnect(headers Headers) error
	Nack(headers Headers) error
	Send(Headers, string) error
	Subscribe(headers Headers) (<-chan MessageData, error)
	Unsubscribe(headers Headers) error
	//
	SendBytes(h Headers, b []byte) error
}

/*
	StatsReader is an interface that modela a reader for the statistics
	maintained by the stompngo package.
*/
type StatsReader interface {
	FramesRead() int64
	BytesRead() int64
	FramesWritten() int64
	BytesWritten() int64
}

/*
	HBDataReader is an interface that modela a reader for the heart beat
	data maintained by the stompngo package.
*/
type HBDataReader interface {
	SendTickerInterval() int64
	ReceiveTickerInterval() int64
	SendTickerCount() int64
	ReceiveTickerCount() int64
}

/*
	Deadliner is an interface that models the optional network deadline
	functionality implemented by the stompngo package.
*/
type Deadliner interface {
	WriteDeadline(d time.Duration)
	EnableWriteDeadline(e bool)
	ExpiredNotification(enf ExpiredNotification)
	IsWriteDeadlineEnabled() bool
	ReadDeadline(d time.Duration)
	EnableReadDeadline(e bool)
	IsReadDeadlineEnabled() bool
	ShortWriteRecovery(ro bool)
}

/*
	Monitor is an interface that models monitoring a stompngo connection.
*/
type Monitor interface {
	Connected() bool
	Session() string
	Protocol() string
	Running() time.Duration
	SubChanCap() int
}

/*
	ParmHandler is an interface that models stompngo client parameter
	specification.
*/
type ParmHandler interface {
	SetLogger(l *log.Logger)
	SetSubChanCap(nc int)
}

/*
	STOMPConnector is an interface that encapsulates the Connection struct.
*/
type STOMPConnector interface {
	Stomper
	StatsReader
	HBDataReader
	Deadliner
	Monitor
	ParmHandler
	//
}

/*
	Connection is a representation of a STOMP connection.
*/
type Connection struct {
	ConnectResponse   *Message           // Broker response (CONNECTED/ERROR) if physical connection successful.
	DisconnectReceipt MessageData        // If receipt requested on DISCONNECT.
	MessageData       <-chan MessageData // Inbound data for the client.
	connected         bool
	session           string
	protocol          string
	input             chan MessageData
	output            chan wiredata
	netconn           net.Conn
	subs              map[string]*subscription
	subsLock          sync.RWMutex
	ssdc              chan struct{} // System shutdown channel
	wtrsdc            chan struct{} // Special writer shutdown channel
	hbd               *heartBeatData
	wtr               *bufio.Writer
	rdr               *bufio.Reader
	Hbrf              bool // Indicates a heart beat read/receive failure, which is possibly transient.  Valid for 1.1+ only.
	Hbsf              bool // Indicates a heart beat send failure, which is possibly transient.  Valid for 1.1+ only.
	logger            *log.Logger
	mets              *metrics      // Client metrics
	scc               int           // Subscribe channel capacity
	discLock          sync.Mutex    // DISCONNECT lock
	dld               *deadlineData // Deadline data
}

type subscription struct {
	md   chan MessageData // Subscription specific MessageData channel
	id   string           // Subscription id (unique, self reference)
	am   string           // ACK mode for this subscription
	cs   bool             // Closed during shutdown
	drav bool             // Drain After value validity
	dra  uint             // Start draining after # messages (MESSAGE frames)
	drmc uint             // Current drain count if draining
}

/*
	Error definition.
*/
type Error string

/*
	Error constants.
*/
const (
	// ERROR Frame returned by broker on connect.
	ECONERR = Error("broker returned ERROR frame, CONNECT")

	// ERRORs for Headers.
	EHDRLEN  = Error("unmatched headers, bad length")
	EHDRUTF8 = Error("header string not UTF8")
	EHDRNIL  = Error("headers can not be nil")
	EUNKHDR  = Error("corrupt frame headers")
	EHDRMTK  = Error("header key can not be empty")
	EHDRMTV  = Error("header value can not be empty")

	// ERRORs for response to CONNECT.
	EUNKFRM = Error("unrecognized frame returned, CONNECT")
	EBADFRM = Error("Malformed frame")

	// No body allowed error
	EBDYDATA = Error("body data not allowed")

	// Not connected.
	ECONBAD = Error("no current connection or DISCONNECT previously completed")

	// Destination required
	EREQDSTSND = Error("destination required, SEND")
	EREQDSTSUB = Error("destination required, SUBSCRIBE")
	EREQDIUNS  = Error("destination required, UNSUBSCRIBE")
	EREQDSTUNS = Error("destination required, UNSUBSCRIBE") // Alternate name

	// id required
	EREQIDUNS = Error("id required, UNSUBSCRIBE")

	// Message ID required.
	EREQMIDACK = Error("message-id required, ACK") // 1.0, 1.1
	EREQIDACK  = Error("id required, ACK")         // 1.2

	// Subscription required.
	EREQSUBACK = Error("subscription required, ACK") // 1.1

	// NACK's.  STOMP 1.1 or greater.
	EREQMIDNAK = Error("message-id required, NACK")   // 1.1
	EREQSUBNAK = Error("subscription required, NACK") // 1.1
	EREQIDNAK  = Error("id required, NACK")           // 1.2

	// Transaction ID required.
	EREQTIDBEG = Error("transaction-id required, BEGIN")
	EREQTIDCOM = Error("transaction-id required, COMMIT")
	EREQTIDABT = Error("transaction-id required, ABORT")

	// Transaction ID present but empty.
	ETIDBEGEMT = Error("transaction-id empty, BEGIN")
	ETIDCOMEMT = Error("transaction-id empty, COMMIT")
	ETIDABTEMT = Error("transaction-id empty, ABORT")

	// Host header required, STOMP 1.1+
	EREQHOST = Error("host header required for STOMP 1.1+")

	// Subscription errors.
	EDUPSID = Error("duplicate subscription-id")
	EBADSID = Error("invalid subscription-id")

	// Subscribe errors.
	ESBADAM = Error("invalid ackmode, SUBSCRIBE")

	// Unsubscribe error.
	EUNOSID  = Error("id required, UNSUBSCRIBE")
	EUNODSID = Error("destination or id required, UNSUBSCRIBE") // 1.0

	// Unsupported version error.
	EBADVERCLI = Error("unsupported protocol version, client")
	EBADVERSVR = Error("unsupported protocol version, server")
	EBADVERNAK = Error("unsupported protocol version, NACK")

	// Unsupported Headers type.
	EBADHDR = Error("unsupported Headers type")

	// Receipt not allowed on connect
	ENORECPT = Error("receipt not allowed on CONNECT")

	// Invalid broker command
	EINVBCMD = Error("invalid broker command")
)

/*
	A zero length buffer for convenience.
*/
var NULLBUFF = make([]uint8, 0)

/*
   A no disconnect receipt Headers value for convenience.
*/
var NoDiscReceipt = Headers{"noreceipt", "true"}

/*
	Codec data structure definition.
*/
type codecdata struct {
	encoded string
	decoded string
}

/*
	STOMP specification defined encoded / decoded values for the Message
	command and headers.
*/
var codecValues = []codecdata{
	codecdata{"\\\\", "\\"},
	codecdata{"\\" + "n", "\n"},
	codecdata{"\\" + "r", "\r"},
	codecdata{"\\c", ":"},
}

/*
	Control data for initialization of heartbeats with STOMP 1.1+, and the
	subsequent control of any heartbeat routines.
*/
type heartBeatData struct {
	sdl  sync.Mutex // Send data lock
	rdl  sync.Mutex // Receive data lock
	clk  sync.Mutex // Shutdown lock
	ssdn bool       // Shutdown complete
	//
	cx int64 // client send value, ms
	cy int64 // client receive value, ms
	sx int64 // server send value, ms
	sy int64 // server receive value, ms
	//
	hbs bool // sending heartbeats
	hbr bool // receiving heartbeats
	//
	sti int64 // local sender ticker interval, ns
	rti int64 // local receiver ticker interval, ns
	//
	sc int64 // local sender ticker count
	rc int64 // local receiver ticker count
	//
	ssd chan struct{} // sender shutdown channel
	rsd chan struct{} // receiver shutdown channel
	//
	ls int64 // last send time, ns
	lr int64 // last receive time, ns
}

/*
	Control structure for basic client metrics.
*/
type metrics struct {
	st  time.Time // Start Time
	tfr int64     // Total frame reads
	tbr int64     // Total bytes read
	tfw int64     // Total frame writes
	tbw int64     // Total bytes written
}

/*
  Valid broker commands.
*/
var validCmds = map[string]bool{MESSAGE: true, ERROR: true, RECEIPT: true}

var logLock sync.Mutex

const (
	NetProtoTCP = "tcp" // Protocol Name
)

/*
	Common Header keys
*/
const (
	HK_ACCEPT_VERSION = "accept-version"
	HK_ACK            = "ack"
	HK_CONTENT_TYPE   = "content-type"
	HK_CONTENT_LENGTH = "content-length"
	HK_DESTINATION    = "destination"
	HK_HEART_BEAT     = "heart-beat"
	HK_HOST           = "host" // HK_VHOST aloas
	HK_ID             = "id"
	HK_LOGIN          = "login"
	HK_MESSAGE        = "message"
	HK_MESSAGE_ID     = "message-id"
	HK_SUPPRESS_CL    = "suppress-content-length" // Not in any spec, but used
	HK_SUPPRESS_CT    = "suppress-content-type"   // Not in any spec, but used
	HK_PASSCODE       = "passcode"
	HK_RECEIPT        = "receipt"
	HK_RECEIPT_ID     = "receipt-id"
	HK_SESSION        = "session"
	HK_SERVER         = "server"
	HK_SUBSCRIPTION   = "subscription"
	HK_TRANSACTION    = "transaction"
	HK_VERSION        = "version"
	HK_VHOST          = "host" // HK_HOST alias
)

/*
	ACK Modes
*/
const (
	AckModeAuto             = "auto"
	AckModeClient           = "client"
	AckModeClientIndividual = "client-individual"
)

var (
	validAckModes10 = map[string]bool{AckModeAuto: true,
		AckModeClient: true}
	validAckModes1x = map[string]bool{AckModeClientIndividual: true}
)

/*
	Default content-type.
*/
const (
	DFLT_CONTENT_TYPE = "text/plain; charset=UTF-8"
)

/*
	Extensions to STOMP protocol.
*/
const (
	StompPlusDrainAfter = "sng_drafter" // SUBSCRIBE Header
)

var (
	LFB = []byte("\n")
	ZRB = []byte{0}
)
