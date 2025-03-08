// Copyright 2025, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Simple library for retry mechanism.
//
// Inspired by [retry-go](https://github.com/avast/retry-go)

package retry

import (
	"context"
	"math/rand"
	"time"
)

// Function signature of retryable function
type RetryableFunc func() error

func Do(ctx context.Context, retryableFunc RetryableFunc, opts ...Option) error {
	var n uint

	if err := ctx.Err(); err != nil {
		return err
	}

	// Set config
	config := newDefaultRetryConfig()
	for _, opt := range opts {
		opt(config)
	}

	for {
		n++

		err := retryableFunc()
		if err == nil {
			return nil
		}

		config.attempts = config.dynamicAttempts(n, config.attempts, err)
		// if this is last attempt or we now have less attempts that tries - return immediately
		if config.attempts != 0 && n >= config.attempts {
			return err
		}

		if !config.retryIf(n, err) {
			return err
		}
		if err := config.onRetry(n, err); err != nil {
			return err
		}

		config.delay = config.dynamicDelay(n, config.delay, err)
		select {
		case <-config.after(config.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func Jitter(d time.Duration) time.Duration {
	f := float64(d)
	f /= 50
	f += f * (rand.Float64() - 0.5)
	return time.Duration(f)
}

func Backoff(d time.Duration) time.Duration {
	if d > 30*time.Second {
		return 30*time.Second + Jitter(d)
	}
	return d*2 + Jitter(d*2)
}

func newDefaultRetryConfig() *Config {
	return &Config{
		attempts: uint(1),
		delay:    0,

		dynamicAttempts: func(attempt uint, attempts uint, err error) uint { return attempts },
		dynamicDelay:    func(attempt uint, delay time.Duration, err error) time.Duration { return delay },

		onRetry: func(attempt uint, err error) error { return nil },
		retryIf: func(attempt uint, err error) bool { return true },

		after: time.After,
	}
}
