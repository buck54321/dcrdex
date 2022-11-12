package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ResolveTCPAddr error: %v \n", err)
		return
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListenTCP error: %v \n", err)
		return
	}

	fmt.Printf("\n     Go to http://%s \n\n", l.Addr())

	l.Close()
	err = http.ListenAndServe(l.Addr().String(), http.FileServer(http.Dir("site")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListenAndServe error: %v \n", err)
		return
	}
}
