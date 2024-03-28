package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"
)

var (
	DefaultBlows         = 10_000
	DefaultStatsInterval = 1 * time.Second
	DefaultAbortAfter    = 1 * time.Second
)

type domains []string

func (d *domains) String() string {
	return fmt.Sprintf("%s", *d)
}

func (d *domains) Set(val string) error {
	*d = append(*d, val)
	return nil
}

type Result struct {
	duration time.Duration
	err      error
}

type Stats struct {
	min, max, avg, total time.Duration
	count, errCount      int
}

func (s Stats) String() string {
	return fmt.Sprintf("total: %d min: %s max: %s avg: %s errors: %d",
		s.count,
		s.min,
		s.max,
		s.avg,
		s.errCount,
	)
}

type Config struct {
	domains       domains
	concurrency   int
	blows         int
	statsInterval time.Duration
	abortAfter    time.Duration
}

func (c *Config) Validate() error {
	return nil
}

func info(out io.Writer, cfg Config) {
	fmt.Fprintln(out, "╔══════════════════════╗")
	fmt.Fprintln(out, "║ 🔨 DNS HAMMER v0.0.1 ║")
	fmt.Fprintln(out, "╚══════════════════════╝")
	fmt.Fprintf(out, "> Domains: %s\n", strings.Join(cfg.domains, "\n          "))
	fmt.Fprintf(out, "> Hammer Blows: %d\n", cfg.blows)
	fmt.Fprintf(out, "> Concurrency: %d\n", cfg.concurrency)
	fmt.Fprintf(out, "> Stats Interval: %s\n", cfg.statsInterval)
	fmt.Fprintf(out, "> Abort After: %s\n", cfg.abortAfter)
	fmt.Fprintln(out, "")
}

func resolver(ctx context.Context, r *net.Resolver, domains <-chan string, results chan<- Result) {
	for domain := range domains {
		start := time.Now()
		_, err := r.LookupIPAddr(ctx, domain)
		results <- Result{time.Since(start), err}
	}
}

func run(ctx context.Context, out io.Writer, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	cfg := Config{}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.Var(&cfg.domains, "domain", "TODO")
	flags.IntVar(&cfg.blows, "blows", DefaultBlows, "TODO")
	flags.IntVar(&cfg.concurrency, "concurrency", runtime.NumCPU(), "TODO")
	flags.DurationVar(&cfg.statsInterval, "stats-interval", DefaultStatsInterval, "TODO")
	flags.DurationVar(&cfg.abortAfter, "abort-after", DefaultAbortAfter, "TODO")

	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	r := &net.Resolver{}
	stats := Stats{}

	info(out, cfg)

	d := make(chan string, cfg.blows)
	res := make(chan Result, cfg.blows)

	for worker := 0; worker < cfg.concurrency; worker++ {
		go resolver(ctx, r, d, res)
	}

	ticker := time.NewTicker(cfg.statsInterval)
	tickerDone := make(chan bool)

	go func() {
		for {
			select {
			case <-tickerDone:
				return
			case <-ticker.C:
				fmt.Fprintln(out, stats.String())
			}
		}
	}()

	for i := 0; i < cfg.blows; i++ {
		// cycle through domains in list...
		d <- cfg.domains[i%len(cfg.domains)]
	}
	close(d)

	for a := 0; a < cfg.blows; a++ {
		result := <-res

		stats.total += result.duration
		if stats.min == 0 {
			stats.min = result.duration
		}
		stats.min = min(result.duration, stats.min)
		stats.max = max(result.duration, stats.max)
		stats.count++

		if result.err != nil {
			stats.errCount++
		}

		stats.avg = stats.total / time.Duration(stats.count)

	}
	ticker.Stop()
	tickerDone <- true
	fmt.Fprintln(out, stats.String())
	return nil
}

func main() {
	ctx := context.Background()

	if err := run(ctx, os.Stdout, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
