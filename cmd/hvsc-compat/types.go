package main

// Contains shared scan configuration, result, and diagnostic types.

import "fmt"

const (
	tokenSys = 0x9e
	tokenGo  = 0xcb
)

type tuneJob struct {
	Path string
	Rel  string
}

type fileResult struct {
	Tested   int
	Failures []failure
}

type failure struct {
	Bucket         string
	Phase          string
	Kind           string
	Path           string
	Subtune        int
	DefaultTune    int
	Subtunes       int
	Title          string
	Author         string
	Released       string
	Format         string
	Types          string
	Version        int
	Clock          string
	SIDModel       string
	Load           string
	Init           string
	Play           string
	Flags          string
	Speed          int
	Basic          bool
	MUS            bool
	PayloadBytes   int
	SonglengthMD5  string
	Entry          string
	PC             string
	Opcode         string
	Mnemonic       string
	Cycles         int
	MaxCycles      int
	CyclesPerFrame string
	BankRegister   string
	MemoryClass    string
	Loaded         bool
	IRQHardware    string
	IRQKernal      string
	IRQSelected    string
	IRQSource      string
	RMS            string
	MinRMS         string
	SIDWrites      int
	FirstSIDWrite  string
	LastSIDWrite   string
	SilenceClass   string
	Error          string
}

type silenceError struct {
	RMS         float64
	MinRMS      float64
	Diagnostics silenceDiagnostics
}

func (e *silenceError) Error() string {
	return fmt.Sprintf("render RMS %.6f below min-rms %.6f", e.RMS, e.MinRMS)
}

type silenceDiagnostics struct {
	SIDWrites     int
	FirstSIDWrite int64
	LastSIDWrite  int64
	PC            uint16
	BankRegister  byte
	MemoryClass   string
	Loaded        bool
}
