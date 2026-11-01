//go:build !windows

package main

import "errors"

func run([]string) error { return errors.New("служба есть только для Windows") }
