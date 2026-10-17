//go:build !darwin && !linux

package filetransport

import "os"

func openDirectory(string) (*os.File, error)                          { return nil, ErrUnsafe }
func dupDirectory(*os.File) (*os.File, error)                         { return nil, ErrUnsafe }
func openDirectoryAt(*os.File, string) (*os.File, error)              { return nil, ErrUnsafe }
func openFileAt(*os.File, string, int, os.FileMode) (*os.File, error) { return nil, ErrUnsafe }
func directoryOwned(*os.File) error                                   { return ErrUnsafe }
func regularOwned(*os.File) error                                     { return ErrUnsafe }
func renameAt(*os.File, string, *os.File, string) error               { return ErrUnsafe }
func removeAt(*os.File, string) error                                 { return ErrUnsafe }
func removeDirAt(*os.File, string) error                              { return ErrUnsafe }
func mkdirAt(*os.File, string, os.FileMode) error                     { return ErrUnsafe }
func symlinkAt(string, *os.File, string) error                        { return ErrUnsafe }

func readlinkAt(*os.File, string) (string, error) { return "", ErrUnsafe }

func linkAt(*os.File, string, *os.File, string) error { return ErrUnsafe }
