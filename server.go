// Package mbserver implments a Modbus server (slave).
package mbserver

import (
	"bytes"
	"encoding/gob"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"

	"github.com/goburrow/serial"
)

const (
	checkpointFile = "modbus.state"
)

// Server is a Modbus slave with allocated memory for discrete inputs, coils, etc.
type Server struct {
	// Debug enables more verbose messaging.
	Debug            bool
	listeners        []net.Listener
	ports            []serial.Port
	requestChan      chan *Request
	function         [256](func(*Server, Framer) ([]byte, *Exception))
	DiscreteInputs   []byte
	Coils            []byte
	HoldingRegisters []uint16
	InputRegisters   []uint16
}

// Request contains the connection and Modbus frame.
type Request struct {
	conn  io.ReadWriteCloser
	frame Framer
}

// NewServer creates a new Modbus server (slave).
func NewServer() *Server {
	s := &Server{}

	// Allocate Modbus memory maps.
	s.DiscreteInputs = make([]byte, 65536)
	s.Coils = make([]byte, 65536)
	s.HoldingRegisters = make([]uint16, 65536)
	s.InputRegisters = make([]uint16, 65536)

	// Add default functions.
	s.function[1] = ReadCoils
	s.function[2] = ReadDiscreteInputs
	s.function[3] = ReadHoldingRegisters
	s.function[4] = ReadInputRegisters
	s.function[5] = WriteSingleCoil
	s.function[6] = WriteHoldingRegister
	s.function[15] = WriteMultipleCoils
	s.function[16] = WriteHoldingRegisters

	// attempt to restore state
	s.restoreState()

	s.requestChan = make(chan *Request)
	go s.handler()

	return s
}

// RegisterFunctionHandler override the default behavior for a given Modbus function.
func (s *Server) RegisterFunctionHandler(funcCode uint8, function func(*Server, Framer) ([]byte, *Exception)) {
	s.function[funcCode] = function
}

func (s *Server) handle(request *Request) Framer {
	var exception *Exception
	var data []byte

	log.Printf("function: %v, value: %+v\n", request.frame.GetFunction(), request.frame.GetData())

	response := request.frame.Copy()

	function := request.frame.GetFunction()
	if s.function[function] != nil {
		data, exception = s.function[function](s, request.frame)
		response.SetData(data)
	} else {
		exception = &IllegalFunction
	}

	if exception != &Success {
		response.SetException(exception)
	}

	return response
}

// All requests are handled synchronously to prevent modbus memory corruption.
func (s *Server) handler() {
	for {
		request := <-s.requestChan
		response := s.handle(request)
		request.conn.Write(response.Bytes())
	}
}

// Close stops listening to TCP/IP ports and closes serial ports.
func (s *Server) Close() {
	for _, listen := range s.listeners {
		listen.Close()
	}
	for _, port := range s.ports {
		port.Close()
	}
}

type StateObject struct {
	DiscreteInputs   []byte
	Coils            []byte
	HoldingRegisters []uint16
	InputRegisters   []uint16
}

func (s *Server) saveState() {
	log.Println("saving state . . .")
	defer log.Println("done")

	so := StateObject{
		DiscreteInputs:   s.DiscreteInputs,
		Coils:            s.Coils,
		HoldingRegisters: s.HoldingRegisters,
		InputRegisters:   s.InputRegisters,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(so)
	if err != nil {
		log.Fatal(err)
	}
	err = ioutil.WriteFile(checkpointFile, buf.Bytes(), 0644)
	if err != nil {
		log.Fatal(err)
	}
}

func (s *Server) restoreState() {
	log.Println("restoring state . . .")
	defer log.Println("done")

	if _, err := os.Stat(checkpointFile); !os.IsNotExist(err) {
		f, err := os.Open(checkpointFile)
		if err != nil {
			log.Fatal(err)
		}
		var so StateObject
		dec := gob.NewDecoder(f)
		err = dec.Decode(&so)
		if err != nil {
			log.Fatal(err)
		}

		s.DiscreteInputs = so.DiscreteInputs
		s.Coils = so.Coils
		s.HoldingRegisters = so.HoldingRegisters
		s.InputRegisters = so.InputRegisters
	}
}
