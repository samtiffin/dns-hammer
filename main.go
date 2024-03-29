package main

import (
	"context"
	"errors"
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
	return fmt.Sprintf("total: %s min: %s max: %s avg: %s requests: %d errors: %d",
		s.total,
		s.min,
		s.max,
		s.avg,
		s.count,
		s.errCount,
	)
}

type Config struct {
	Domains       domains
	Concurrency   int
	Blows         int
	StatsInterval time.Duration
	// TODO: not implemented
	// AbortAfter    time.Duration
}

func (c *Config) Validate() error {
	errs := []error{}

	if len(c.Domains) == 0 {
		errs = append(errs, errors.New("must provide at least one domain"))
	}

	if c.Concurrency < 1 {
		errs = append(errs, errors.New("concurrency must be >= 1"))
	}

	if c.Blows < 0 {
		errs = append(errs, errors.New("blows must be a positive integer"))
	}

	if c.StatsInterval < 0 {
		errs = append(errs, errors.New("stats interval must be a positive duration"))
	}

	// TODO: not implemented
	//if c.AbortAfter < 0 {
	//  err = append(errs, errors.New("abort after must be a positive duration"))
	//}

	return errors.Join(errs...)
}

func info(out io.Writer, cfg Config) {
	fmt.Fprintln(out, "╔══════════════════════╗")
	fmt.Fprintln(out, "║ 🔨 DNS HAMMER v0.0.1 ║")
	fmt.Fprintln(out, "╚══════════════════════╝")
	fmt.Fprintf(out, "> Domains: %s\n", strings.Join(cfg.Domains, "\n           "))
	fmt.Fprintf(out, "> Hammer Blows: %d\n", cfg.Blows)
	fmt.Fprintf(out, "> Concurrency: %d\n", cfg.Concurrency)
	fmt.Fprintf(out, "> Stats Interval: %s\n", cfg.StatsInterval)
	// TODO: not implemented
	// fmt.Fprintf(out, "> Abort After: %s\n", cfg.AbortAfter)
	fmt.Fprintln(out, "════╣ HAMMER TIME ╠════")
}

func resolver(ctx context.Context, r *net.Resolver, domains <-chan string, results chan<- Result) {
	for domain := range domains {
		start := time.Now()
		_, err := r.LookupIPAddr(ctx, domain)
		results <- Result{time.Since(start), err}
	}
}

func run(ctx context.Context, out, errOut io.Writer, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	cfg := Config{}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.Var(&cfg.Domains, "domain", "Domain(s) to use to hammer DNS server with. Pass -domain arg again for each domain.")
	flags.IntVar(&cfg.Blows, "blows", DefaultBlows, "Number of times to hit the DNS server.")
	flags.IntVar(&cfg.Concurrency, "concurrency", runtime.NumCPU(), "How many hammers to use at once, defaults to number of cpus.")
	flags.DurationVar(&cfg.StatsInterval, "stats-interval", DefaultStatsInterval, "How often to report stats.")
	// TODO: not implemented
	// flags.DurationVar(&cfg.AbortAfter, "abort-after", DefaultAbortAfter, "Abort hammering if a request exceeds this time (NOT IMPLEMENTED")

	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	r := &net.Resolver{}
	stats := Stats{}
	var lastErr error

	info(out, cfg)

	d := make(chan string, cfg.Blows)
	res := make(chan Result, cfg.Blows)

	for worker := 0; worker < cfg.Concurrency; worker++ {
		go resolver(ctx, r, d, res)
	}

	ticker := time.NewTicker(cfg.StatsInterval)
	tickerDone := make(chan bool)

	go func() {
		for {
			select {
			case <-tickerDone:
				return
			case <-ticker.C:
				fmt.Fprintln(out, stats.String())
				if lastErr != nil {
					fmt.Fprintln(errOut, lastErr)
				}
			}
		}
	}()

	for i := 0; i < cfg.Blows; i++ {
		// cycle through domains in list...
		d <- cfg.Domains[i%len(cfg.Domains)]
	}
	close(d)

	for a := 0; a < cfg.Blows; a++ {
		result := <-res

		stats.total += result.duration
		if stats.min == 0 {
			stats.min = result.duration
		}
		stats.min = min(result.duration, stats.min)
		stats.max = max(result.duration, stats.max)
		stats.count++

		if result.err != nil {
			lastErr = result.err
			stats.errCount++
		}

		stats.avg = stats.total / time.Duration(stats.count)
	}

	ticker.Stop()
	tickerDone <- true
	fmt.Fprintln(out, stats.String())
	if lastErr != nil {
		fmt.Fprintln(errOut, lastErr)
	}

	fmt.Fprintln(out, "════╣ STOP ╠════")
	return nil
}

func main() {
	ctx := context.Background()

	if err := run(ctx, os.Stdout, os.Stderr, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
