package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/stretchrcom/testify/assert"
	"net/http"
	"testing"
	"time"
)

type RestExample struct {
	Service `prefix:"/prefix" mime:"application/json" charset:"utf-8"`

	CreateHello Processor `method:"POST" path:"/hello"`
	GetHello    Processor `method:"GET" path:"/hello/:to" func:"HandleHello"`
	Watch       Streaming `method:"GET" path:"/hello/:to/streaming"`

	post  map[string]string
	watch map[string]chan string
}

type HelloArg struct {
	To   string `json:"to"`
	Post string `json:"post"`
}

// Post example:
// > curl "http://127.0.0.1:8080/prefix/hello" -d '{"to":"rest", "post":"rest is powerful"}'
//
// No response
func (r RestExample) HandleCreateHello(arg HelloArg) {
	r.post[arg.To] = arg.Post
	c, ok := r.watch[arg.To]
	if ok {
		select {
		case c <- arg.Post:
		default:
		}
	}
}

// Get example:
// > curl "http://127.0.0.1:8080/prefix/hello/rest"
//
// Response:
//   {"to":"rest","post":"rest is powerful"}
func (r RestExample) HandleHello() HelloArg {
	if r.Vars() == nil {
		r.Error(http.StatusNotFound, fmt.Errorf("%+v", r.Vars()))
		return HelloArg{}
	}
	to := r.Vars()["to"]
	post, ok := r.post[to]
	if !ok {
		r.Error(http.StatusNotFound, fmt.Errorf("can't find hello to %s", to))
		return HelloArg{}
	}
	return HelloArg{
		To:   to,
		Post: post,
	}
}

// Streaming example:
// > curl "http://127.0.0.1:8080/prefix/hello/rest/streaming"
//
// It create a long-live connection and will receive post content "rest is powerful"
// when running post example.
func (r RestExample) HandleWatch(s Stream) {
	to := r.Vars()["to"]
	if to == "" {
		r.Error(http.StatusBadRequest, fmt.Errorf("need to"))
		return
	}
	r.WriteHeader(http.StatusOK)
	c := make(chan string)
	r.watch[to] = c
	for {
		post := <-c
		s.SetDeadline(time.Now().Add(time.Second))
		err := s.Write(post)
		if err != nil {
			close(c)
			delete(r.watch, to)
			return
		}
	}
}

func TestExample(t *testing.T) {
	instance := &RestExample{
		post:  make(map[string]string),
		watch: make(map[string]chan string),
	}
	rest, err := New(instance)
	if err != nil {
		t.Fatalf("create rest failed: %s", err)
	}

	assert.Equal(t, rest.Prefix(), "/prefix")

	go http.ListenAndServe("127.0.0.1:12345", rest)

	resp, err := http.Get("http://127.0.0.1:12345/prefix/hello/rest")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)

	c := make(chan int)
	go func() {
		resp, err := http.Get("http://127.0.0.1:12345/prefix/hello/rest/streaming")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		assert.Equal(t, resp.StatusCode, http.StatusOK)

		expect := "\"rest is powerful\"\n"
		get := make([]byte, len(expect))
		n, err := resp.Body.Read(get)
		if err != nil {
			t.Fatal(err)
		}
		get = get[:n]
		assert.Equal(t, string(get), expect)

		c <- 1
	}()

	time.Sleep(time.Second / 2) // waiting streaming connected

	arg := HelloArg{
		To:   "rest",
		Post: "rest is powerful",
	}
	buf := bytes.NewBuffer(nil)
	encoder := json.NewEncoder(buf)
	err = encoder.Encode(arg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post("http://127.0.0.1:12345/prefix/hello", "application/json", buf)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case <-c:
	case <-time.After(time.Second):
		t.Errorf("waiting streaming too long")
	}

	resp, err = http.Get("http://127.0.0.1:12345/prefix/hello/rest")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	assert.Equal(t, resp.StatusCode, http.StatusOK)

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&arg)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, arg.To, "rest")
	assert.Equal(t, arg.Post, "rest is powerful")
}
