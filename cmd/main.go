package main

import (
    "fmt"
    "os"

    "github.com/ripsline/virtual-private-node/internal/config"
    "github.com/ripsline/virtual-private-node/internal/installer"
    "github.com/ripsline/virtual-private-node/internal/welcome"
)

const version = "0.1.0"

func main() {
    // If the node is already installed, show the welcome
    // message and drop to shell. This runs on every SSH login.
    if !installer.NeedsInstall() {
        cfg, err := config.Load()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
            cfg = config.Default()
        }
        welcome.Show(cfg, version)
        return
    }

    // First run — start the installer.
    // Must be root (called via sudo from .bash_profile).
    if os.Geteuid() != 0 {
        fmt.Println("ERROR: Installer must run as root.")
        fmt.Println("Run with: sudo rlvpn")
        os.Exit(1)
    }

    fmt.Printf("\n  ╔══════════════════════════════════════════╗\n")
    fmt.Printf("  ║  Virtual Private Node v%-17s ║\n", version)
    fmt.Printf("  ╚══════════════════════════════════════════╝\n\n")

    if err := installer.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "\n  Installation failed: %v\n", err)
        os.Exit(1)
    }
}