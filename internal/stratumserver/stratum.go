package stratumserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"detalki-chain/internal/blockchain"
)

type Server struct {
	mu       sync.Mutex
	bc       *blockchain.Blockchain
	listener net.Listener
	port     int
	miner    string
	clients  map[net.Conn]*session
	template *blockchain.BlockTemplate
}

type session struct {
	conn   net.Conn
	writer *bufio.Writer
}

func New(bc *blockchain.Blockchain, port int, minerAddr string) *Server {
	return &Server{
		bc:      bc,
		port:    port,
		miner:   minerAddr,
		clients: make(map[net.Conn]*session),
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("stratum: %w", err)
	}
	s.listener = ln
	s.refreshTemplate()
	log.Printf("Stratum-прокси: слушаю на :%d (подключи Apollo сюда)", s.port)
	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for c := range s.clients {
		c.Close()
	}
	s.mu.Unlock()
	log.Println("Stratum-прокси: остановлен")
}

func (s *Server) refreshTemplate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.template = s.bc.GetBlockTemplate(s.miner)
	log.Printf("Stratum-прокси: шаблон блока #%d готов", s.template.Block.Index)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleClient(conn)
	}
}

func (s *Server) handleClient(conn net.Conn) {
	defer conn.Close()
	sess := &session{conn: conn, writer: bufio.NewWriter(conn)}
	s.mu.Lock()
	s.clients[conn] = sess
	s.mu.Unlock()
	log.Printf("Stratum-прокси: Apollo подключился (%s)", conn.RemoteAddr())
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 4096)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		s.handleRequest(sess, &req)
	}
	s.mu.Lock()
	delete(s.clients, conn)
	s.mu.Unlock()
	log.Println("Stratum-прокси: Apollo отключился")
}

func (s *Server) handleRequest(sess *session, req *rpcRequest) {
	switch req.Method {
	case "mining.subscribe":
		resp := rpcResponse{ID: req.ID, Result: []any{
			[]any{[]any{"mining.set_difficulty", "1"}, []any{"mining.notify", "1"}},
			"00000000", 4,
		}}
		send(sess.writer, resp)
		send(sess.writer, rpcNotify{Method: "mining.set_difficulty", Params: []any{1.0}})
	case "mining.authorize":
		resp := rpcResponse{ID: req.ID, Result: []any{true, "OK"}}
		send(sess.writer, resp)
		s.sendJob(sess)
	case "mining.submit":
		s.handleSubmit(sess, req)
	}
}

func (s *Server) handleSubmit(sess *session, req *rpcRequest) {
	if len(req.Params) < 5 {
		send(sess.writer, rpcResponse{ID: req.ID, Result: []any{false, "bad params"}})
		return
	}
	nonceHex, _ := req.Params[4].(string)
	nonceBytes, err := hexDecode(nonceHex)
	if err != nil || len(nonceBytes) != 4 {
		send(sess.writer, rpcResponse{ID: req.ID, Result: []any{false, "bad nonce"}})
		return
	}
	nonce := uint32(nonceBytes[0])<<24 | uint32(nonceBytes[1])<<16 | uint32(nonceBytes[2])<<8 | uint32(nonceBytes[3])
	ntimeHex, _ := req.Params[3].(string)
	ntimeBytes, _ := hexDecode(ntimeHex)
	var ntime uint32
	if len(ntimeBytes) >= 4 {
		ntime = uint32(ntimeBytes[0])<<24 | uint32(ntimeBytes[1])<<16 | uint32(ntimeBytes[2])<<8 | uint32(ntimeBytes[3])
	}
	s.mu.Lock()
	tmpl := s.template
	s.mu.Unlock()
	if tmpl == nil {
		send(sess.writer, rpcResponse{ID: req.ID, Result: []any{false, "no template"}})
		return
	}
        block, ok := s.bc.SubmitNoncePoC(tmpl, nonce, ntime)

	if !ok {
		send(sess.writer, rpcResponse{ID: req.ID, Result: []any{false, "low share"}})
		return
	}
	log.Printf("Stratum-прокси: БЛОК #%d НАЙДЕН! nonce=%d", block.Index, nonce)
	send(sess.writer, rpcResponse{ID: req.ID, Result: []any{true, "block found"}})
	s.refreshTemplate()
	s.mu.Lock()
	for _, c := range s.clients {
		s.sendJob(c)
	}
	s.mu.Unlock()
}

func (s *Server) sendJob(sess *session) {
	s.mu.Lock()
	tmpl := s.template
	s.mu.Unlock()
	if tmpl == nil {
		return
	}
	prevHash := hexEncode(tmpl.Header[4:36])
	coinb1 := hexEncode(tmpl.Header[36:68])
	notify := rpcNotify{Method: "mining.notify", Params: []any{
		fmt.Sprintf("dtk_%d", tmpl.Block.Index), prevHash, coinb1, "00000000",
		[]any{}, hexEncode(tmpl.Header[0:4]), hexEncode(tmpl.Header[72:76]),
		hexEncode(tmpl.Header[68:72]), true,
	}}
	send(sess.writer, notify)
}

type rpcRequest struct {
	ID     int      `json:"id"`
	Method string   `json:"method"`
	Params []any    `json:"params"`
}

type rpcResponse struct {
	ID     int    `json:"id"`
	Result []any  `json:"result"`
	Error  any    `json:"error"`
}

type rpcNotify struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

func send(w *bufio.Writer, v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	w.Write(data)
	w.Flush()
}

func hexEncode(b []byte) string {
	const hexc = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexc[v>>4]
		out[i*2+1] = hexc[v&0xf]
	}
	return string(out)
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := hexNibble(s[i])
		lo := hexNibble(s[i+1])
		if hi < 0 || lo < 0 {
			return nil, fmt.Errorf("bad hex")
		}
		out[i/2] = byte(hi<<4 | lo)
	}
	return out, nil
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}
