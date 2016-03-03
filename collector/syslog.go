package collector

import (
	"bufio"
	"io"
	"net"
	"strings"
	"time"

	"github.com/jeromer/syslogparser"
	"github.com/jeromer/syslogparser/rfc3164"
	"github.com/jeromer/syslogparser/rfc5424"

	"github.com/nanopack/logvac/config"
	"github.com/nanopack/logvac/core"
)

type (
	// fakeSyslog is a catch-all for non-rfc data collected
	fakeSyslog struct {
		data []byte
	}
)

//Map syslog levels to logging levels (FYI, they don't really match well)
var adjust = []int{
	5, // Alert         -> FATAL
	5, // Critical      -> FATAL
	5, // Emergency     -> FATAL
	4, // Error         -> ERROR
	3, // Warning       -> WARN
	2, // Notice        -> INFO
	2, // Informational -> INFO
	1, // Debug         -> DEBUG
}

// SyslogUDPStart begins listening to the syslog port, transfers all
// syslog messages on the wChan
func SyslogUDPStart(kind, address string) error {
	parsedAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return err
	}
	socket, err := net.ListenUDP("udp", parsedAddress)
	if err != nil {
		return err
	}
	go func() {
		var buf []byte = make([]byte, 1024)
		for {
			n, remote, err := socket.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if remote != nil {
				// if the number of bytes read is greater than 0
				if n > 0 {
					// handle parsing in another process so that this one can continue to receive
					// UDP packets
					go func(buf []byte) {
						msg := parseMessage(buf[0:n])
						msg.Type = kind
						logvac.WriteMessage(msg)
					}(buf)
				}
			}
		}
	}()

	return nil
}

func SyslogTCPStart(kind, address string) error {
	serverSocket, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := serverSocket.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn, kind)
		}
	}()
	return nil
}

func handleConnection(conn net.Conn, kind string) {
	r := bufio.NewReader(conn)

	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			// some unexpected error happened
			return
		}

		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		msg := parseMessage([]byte(line))
		msg.Type = kind
		logvac.WriteMessage(msg)
	}
}

// parseMessage parses the syslog message and returns a msg
// if the msg is not parsable or a standard formatted syslog message
// it will drop the whole message into the content and make up a timestamp
// and a severity
func parseMessage(b []byte) (msg logvac.Message) {
	config.Log.Trace("Raw syslog message: %v", string(b))
	parsers := make([]syslogparser.LogParser, 3)
	parsers[0] = rfc3164.NewParser(b)
	parsers[1] = rfc5424.NewParser(b)
	parsers[2] = &fakeSyslog{b}

	for _, parser := range parsers {
		config.Log.Trace("Trying Parser...")
		err := parser.Parse()
		if err == nil {
			// todo: handle rfc5424 'message' and 'app_name' fields (correspond to content and tag)
			parsedData := parser.Dump()
			config.Log.Trace("Parsed data: %v", parsedData)
			msg.Hostname = parsedData["hostname"].(string)
			msg.Tag = parsedData["tag"].(string)
			msg.Time = parsedData["timestamp"].(time.Time)
			msg.Priority = adjust[parsedData["severity"].(int)] // parser guarantees [0,7]
			tag, ok := parsedData["tag"]
			switch {
			case ok == true:
				msg.Content = tag.(string) + " " + parsedData["content"].(string)
			default:
				msg.Content = parsedData["content"].(string)
			}
			return
		}
	}
	return
}

// just a fake syslog parser
func (fake *fakeSyslog) Parse() error {
	return nil
}

func (fake *fakeSyslog) Dump() syslogparser.LogParts {
	parsed := make(map[string]interface{}, 4)
	parsed["timestamp"] = time.Now()
	parsed["severity"] = 5
	parsed["content"] = string(fake.data)
	return parsed
}

func (fake *fakeSyslog) Location(loc *time.Location) {
	return
}
