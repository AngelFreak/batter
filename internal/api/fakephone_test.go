package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
)

const fakeSerial = "FAKE01"

// useFakePhone puts test/fakeadb on PATH as adb, so the real device manager
// runs real sessions against a simulated phone. Call before newTestEnv.
// It returns the fake's state directory.
func useFakePhone(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "adb"), "../../test/fakeadb")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	state := t.TempDir()
	t.Setenv("FAKEADB_DIR", state)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return state
}

// startFakeSession starts a scrcpy session on the fake phone and returns an
// httptest server for the router (WebSockets need a real one).
func (e *testEnv) startFakeSession(t *testing.T) *httptest.Server {
	t.Helper()
	if w := e.do(t, "admin", "POST", "/api/v1/devices/"+fakeSerial+"/session/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start session: %d %s", w.Code, w.Body.String())
	}
	srv := httptest.NewServer(e.router)
	t.Cleanup(srv.Close)
	return srv
}

func (e *testEnv) dialWS(t *testing.T, srv *httptest.Server, user, kind string) *ws.Conn {
	t.Helper()
	token, err := e.jwt.GenerateToken(e.users[user], user, e.role(t, user))
	if err != nil {
		t.Fatal(err)
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/device/" + fakeSerial + "/" + kind + "?token=" + token
	conn, resp, err := ws.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (status %d)", kind, err, status)
	}
	return conn
}

func resetCount(state string) int {
	b, _ := os.ReadFile(filepath.Join(state, "resets"))
	return strings.Count(string(b), "reset")
}

// Viewers and thumbnails joining together (a dashboard opening) must cost
// one or two encoder resets, not one each, and every viewer must still get
// a keyframe to start decoding.
func TestViewersJoiningTogetherShareKeyframes(t *testing.T) {
	state := useFakePhone(t)
	env := newTestEnv(t)
	srv := env.startFakeSession(t)

	const viewers = 6
	var wg sync.WaitGroup
	for i := range viewers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := env.dialWS(t, srv, "admin", "video")
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					t.Errorf("viewer %d got no keyframe: %v", i, err)
					return
				}
				if len(msg) >= 12 && msg[0]&0x40 != 0 { // keyframe flag (bit 62)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := resetCount(state); n < 1 || n > 2 {
		t.Fatalf("%d encoder resets for %d joining viewers, want 1-2", n, viewers)
	}
}
