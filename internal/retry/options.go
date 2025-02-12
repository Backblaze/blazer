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

package retry

import (
	"time"
)

// Function signature of "dynamic attempts" function
type DynamicAttemptsFunc func(attempt uint, attempts uint, err error) uint

// Function signature of "dynamic delay" function
type DynamicDelayFunc func(attempt uint, delay time.Duration, err error) time.Duration

// Function signature of "on retry" function
type OnRetryFunc func(attempt uint, err error) error

// Function signature of "retry if" function
type RetryIfFunc func(attempt uint, err error) bool

// Function signature of time.After function
type AfterFunc func(time.Duration) <-chan time.Time

type Config struct {
	attempts uint
	delay    time.Duration

	dynamicAttempts DynamicAttemptsFunc
	dynamicDelay    DynamicDelayFunc

	onRetry OnRetryFunc
	retryIf RetryIfFunc

	after AfterFunc
}

// Option represents an option for retry.
type Option func(*Config)

func emptyOption(c *Config) {}

// Attempts set count of retry. Setting to 0 will retry until the retried function succeeds.
// Default is 1 (one attampt)
// Number of attempts can be override by the dynamicAttempts function
func Attempts(attempts uint) Option {
	return func(c *Config) {
		c.attempts = attempts
	}
}

// Delay set delay between retry. It can be override by the delayType function
// Default is 0 (no delay)
// Number of attempts can be override by the dynamicDelay function
func Delay(delay time.Duration) Option {
	return func(c *Config) {
		c.delay = delay
	}
}

// DynamicAttempts dynamically set the number of attempts
func DynamicAttampts(dynamicAttempts DynamicAttemptsFunc) Option {
	if dynamicAttempts == nil {
		return emptyOption
	}
	return func(c *Config) {
		c.dynamicAttempts = dynamicAttempts
	}
}

// DynamicDelay dynamically set the delay between retries
func DynamicDelay(dynamicDelay DynamicDelayFunc) Option {
	if dynamicDelay == nil {
		return emptyOption
	}
	return func(c *Config) {
		c.dynamicDelay = dynamicDelay
	}
}

// OnRetry function callback are called each retry
func OnRetry(onRetry OnRetryFunc) Option {
	if onRetry == nil {
		return emptyOption
	}
	return func(c *Config) {
		c.onRetry = onRetry
	}
}

// RetryIf controls whether a retry should be attempted after an error
// (assuming there are any retry attempts remaining)
func RetryIf(retryIf RetryIfFunc) Option {
	if retryIf == nil {
		return emptyOption
	}
	return func(c *Config) {
		c.retryIf = retryIf
	}
}

// WithAfter provides a way to swap out time.After implementations.
// This primarily is useful for mocking/testing, where you may not want to explicitly wait for a set duration
// for retries.
func WithAfter(after AfterFunc) Option {
	return func(c *Config) {
		c.after = after
	}
}
