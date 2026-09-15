package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTakenControlPortStopsStartup is a second SFU on one machine, finding the control port
// held. It has to exit before its public port can point a server at somebody else's SFU.
func TestTakenControlPortStopsStartup(t *testing.T) {
	if os.Getenv("GRYT_SFU_TEST_MAIN") == "1" {
		main()
		return
	}

	held, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("hold a control port: %v", err)
	}
	defer held.Close()
	controlPort := held.Addr().(*net.TCPAddr).Port
	signallingPort := freePort(t, "tcp")
	mediaPort := freePort(t, "udp")

	cmd := exec.Command(os.Args[0], "-test.run=^TestTakenControlPortStopsStartup$")
	cmd.Env = append(os.Environ(),
		"GRYT_SFU_TEST_MAIN=1",
		fmt.Sprintf("SFU_PORT=%d", signallingPort),
		fmt.Sprintf("SFU_CONTROL_PORT=%d", controlPort),
		fmt.Sprintf("ICE_UDP_MUX_PORT=%d", mediaPort),
		"SFU_METRICS_PORT=0",
		"DISABLE_STUN=true",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the SFU: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stop := func() {
		_ = cmd.Process.Kill()
		<-exited
	}

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.After(30 * time.Second)
	for {
		select {
		case err := <-exited:
			if err == nil {
				t.Fatalf("the SFU exited cleanly with its control port taken:\n%s", output.String())
			}
			if !strings.Contains(output.String(), "SFU_CONTROL_PORT") {
				t.Fatalf("the SFU exited without saying the control port was the problem:\n%s", output.String())
			}
			return
		case <-deadline:
			stop()
			t.Fatalf("the SFU kept running with its control port held by another process:\n%s", output.String())
		case <-time.After(50 * time.Millisecond):
			res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/server", signallingPort))
			if err != nil {
				continue
			}
			res.Body.Close()
			if res.Header.Get("X-Gryt-Control-Port") == strconv.Itoa(controlPort) {
				stop()
				t.Fatalf("the SFU pointed registration at %d, which another process holds:\n%s", controlPort, output.String())
			}
		}
	}
}

func freePort(t *testing.T, network string) int {
	t.Helper()

	if network == "udp" {
		conn, err := net.ListenPacket("udp", ":0")
		if err != nil {
			t.Fatalf("find a free UDP port: %v", err)
		}
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).Port
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("find a free TCP port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
