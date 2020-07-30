// This emulates a simplified behavior of the v0.16.0 Knative autoscaler for
// the purposes of reproducing the behavior of
// https://github.com/knative/serving/issues/8761. This can be built and
// deployed with `ko` to the cluster of your choice. It assumes that the
// network it can probe is the 10.0.0.0/8 space - adjust if the cluster is
// running somewhere else.
//
// NOTE: This will generate HTTP Get requests to random IPs in the network
// range specified.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand" // Predictable is better for this use case.
	"net"
	"net/http"
	"net/url"
	"time"
)

var network = flag.String("network", "10.0.0.0/8", "Network space to probe")

func call(ctx context.Context, ip net.IP) error {
	log.Printf("%s", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s", ip.String()), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if err, ok := err.(*url.Error); ok && err.Op == "Get" {
			if err, ok := err.Err.(*net.OpError); ok && err.Op == "dial" {
				return nil
			}
			if err.Err == context.Canceled || err.Err == context.DeadlineExceeded {
				return err.Err
			}
		}
		return err
	}
	defer resp.Body.Close()

	// Drain response (has in the past made the http client happier - no clue if
	// still needed).
	_, err = io.Copy(ioutil.Discard, resp.Body)
	return err
}

func main() {
	flag.Parse()
	ctx := context.Background()

	_, n, err := net.ParseCIDR(*network)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("%s", n)

	if len(n.IP) != net.IPv4len {
		log.Panic("Only IPv4 supported")
	}

	ones, bits := n.Mask.Size()
	max := 1 << (bits - ones)

	for {
		func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			lower := rand.Int31n(int32(max))
			ip := n.IP.Mask(n.Mask)
			ip[0] |= byte(lower >> 24)
			ip[1] |= byte(lower >> 16)
			ip[2] |= byte(lower >> 8)
			ip[3] |= byte(lower)
			if err := call(ctx, ip); err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				log.Panicf("%T %v", err, err)
			}
		}()
	}
}
