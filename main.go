package main

import (
	"context"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/namsral/flag"
	"golang.org/x/sys/unix"
)

const (
	blockSize = 1 << 9
)

var (
	checkPath     = flag.String("check-path", "", "Path to check")
	checkMode     = flag.String("check-mode", "stat", "Mode of check (stat, read)")
	checkInterval = flag.Float64("check-interval", 5, "Interval between checks (seconds)")
	checkTimeout  = flag.Float64("check-timeout", 10, "Timeout until a succesful check means ready state")
)

type checkFn func(path string) error

func checkStat(path string) error {
	var stat unix.Statfs_t

	return unix.Statfs(path, &stat)
}

func checkRead(path string) error {
	fh, err := os.Open(path)

	if err != nil {
		return err
	}
	defer fh.Close()

	size, err := fh.Seek(0, 2)
	if err != nil {
		return err
	}

	if _, err = fh.Seek(rand.Int63n(size & ^(blockSize-1)), 0); err != nil {
		return err
	}

	buf := make([]byte, blockSize)
	if _, err = fh.Read(buf); err != nil {
		return err
	}

	return nil
}

var (
	ts      time.Time
	tsLock  sync.Mutex
	checkfn checkFn
)

func setts() {
	tsLock.Lock()
	defer tsLock.Unlock()

	ts = time.Now()
}

func ready() bool {
	tsLock.Lock()
	defer tsLock.Unlock()

	return time.Since(ts) < time.Duration(*checkTimeout)*time.Second
}

func main() {
	flag.Parse()

	if *checkPath == "" {
		log.Fatal("Specify -check-path")
	}

	switch *checkMode {
	case "stat":
		checkfn = checkStat
	case "read":
		checkfn = checkRead
	default:
		log.Fatalf("Unsupported check: %s", *checkMode)
	}

	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()

		mux := http.NewServeMux()
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			switch ready() {
			case true:
				w.WriteHeader(200)
			default:
				w.WriteHeader(500)
			}
		})
		server := http.Server{
			Handler: mux,
		}

		go func() {
			defer server.Close()

			<-ctx.Done()
		}()

		server.Serve(lis)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Duration(*checkInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if err := checkfn(*checkPath); err != nil {
				log.Printf("check failed: %+v", err)
			} else {
				setts()
			}
		}

	}()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)

	<-sigchan
	log.Print("Exiting...")

	cancel()

	wg.Wait()
}
