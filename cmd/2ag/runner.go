package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

var version = "dev"

func reserveCDPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve CDP port: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port == 0 {
		_ = listener.Close()
		return 0, fmt.Errorf("reserve CDP port: unexpected listener address %v", listener.Addr())
	}
	port := address.Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release CDP port lease: %w", err)
	}
	return port, nil
}

func setupLogging() func() {
	logPath := "2ag.log"
	if executable, err := os.Executable(); err == nil {
		logPath = filepath.Join(filepath.Dir(executable), "2ag.log")
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.SetOutput(io.Discard)
		return func() {}
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	return func() { _ = file.Close() }
}
