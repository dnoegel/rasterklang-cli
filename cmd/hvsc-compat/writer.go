package main

// Writes compatibility failures as tab-separated report rows.

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type failureWriter struct {
	out    io.WriteCloser
	writer *csv.Writer
}

func openFailureWriter(path string) (*failureWriter, error) {
	var out io.WriteCloser
	if path == "-" {
		out = nopWriteCloser{Writer: os.Stdout}
	} else {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		out = file
	}

	writer := csv.NewWriter(out)
	writer.Comma = '\t'
	if err := writer.Write([]string{
		"bucket",
		"phase",
		"kind",
		"path",
		"subtune",
		"default_subtune",
		"subtunes",
		"title",
		"author",
		"released",
		"format",
		"types",
		"version",
		"clock",
		"sid_model",
		"load",
		"init",
		"play",
		"flags",
		"speed",
		"basic",
		"mus",
		"payload_bytes",
		"songlength_md5",
		"entry",
		"pc",
		"opcode",
		"mnemonic",
		"cycles",
		"max_cycles",
		"cycles_per_frame",
		"bank",
		"memory_class",
		"loaded",
		"irq_hw",
		"irq_kernal",
		"irq_selected",
		"irq_source",
		"rms",
		"min_rms",
		"sid_writes",
		"first_sid_write_s",
		"last_sid_write_s",
		"silence_class",
		"error",
	}); err != nil {
		out.Close()
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		out.Close()
		return nil, err
	}
	return &failureWriter{out: out, writer: writer}, nil
}

func (w *failureWriter) Write(failure failure) error {
	return w.writer.Write([]string{
		cleanField(failure.Bucket),
		cleanField(failure.Phase),
		cleanField(failure.Kind),
		cleanField(failure.Path),
		strconv.Itoa(failure.Subtune),
		strconv.Itoa(failure.DefaultTune),
		strconv.Itoa(failure.Subtunes),
		cleanField(failure.Title),
		cleanField(failure.Author),
		cleanField(failure.Released),
		cleanField(failure.Format),
		cleanField(failure.Types),
		strconv.Itoa(failure.Version),
		cleanField(failure.Clock),
		cleanField(failure.SIDModel),
		cleanField(failure.Load),
		cleanField(failure.Init),
		cleanField(failure.Play),
		cleanField(failure.Flags),
		strconv.Itoa(failure.Speed),
		strconv.FormatBool(failure.Basic),
		strconv.FormatBool(failure.MUS),
		strconv.Itoa(failure.PayloadBytes),
		cleanField(failure.SonglengthMD5),
		cleanField(failure.Entry),
		cleanField(failure.PC),
		cleanField(failure.Opcode),
		cleanField(failure.Mnemonic),
		strconv.Itoa(failure.Cycles),
		strconv.Itoa(failure.MaxCycles),
		cleanField(failure.CyclesPerFrame),
		cleanField(failure.BankRegister),
		cleanField(failure.MemoryClass),
		strconv.FormatBool(failure.Loaded),
		cleanField(failure.IRQHardware),
		cleanField(failure.IRQKernal),
		cleanField(failure.IRQSelected),
		cleanField(failure.IRQSource),
		cleanField(failure.RMS),
		cleanField(failure.MinRMS),
		strconv.Itoa(failure.SIDWrites),
		cleanField(failure.FirstSIDWrite),
		cleanField(failure.LastSIDWrite),
		cleanField(failure.SilenceClass),
		cleanField(failure.Error),
	})
}

func (w *failureWriter) Flush() {
	w.writer.Flush()
}

func (w *failureWriter) Error() error {
	return w.writer.Error()
}

func (w *failureWriter) Close() error {
	w.writer.Flush()
	if err := w.writer.Error(); err != nil {
		_ = w.out.Close()
		return err
	}
	return w.out.Close()
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

var fieldCleaner = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")

func cleanField(value string) string {
	return fieldCleaner.Replace(value)
}
