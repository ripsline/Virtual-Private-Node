// Package installer handles the one-time node setup.
//
// network.go defines the network-specific constants that
// differ between mainnet and testnet4. Every other file
// in this package reads from these values so the network
// choice propagates everywhere automatically.
package installer

// NetworkConfig holds all values that change between networks.
type NetworkConfig struct {
    Name           string // "mainnet" or "testnet4"
    BitcoinFlag    string // config file directive
    LNDBitcoinFlag string // lnd.conf chain flag
    RPCPort        int
    P2PPort        int
    ZMQBlockPort   int
    ZMQTxPort      int
    LNCLINetwork   string // --network flag for lncli
    CookiePath     string // relative to datadir
    DataSubdir     string // subdirectory under /var/lib/bitcoin
}

// Mainnet returns the mainnet configuration.
func Mainnet() *NetworkConfig {
    return &NetworkConfig{
        Name:           "mainnet",
        BitcoinFlag:    "", // no flag needed for mainnet
        LNDBitcoinFlag: "bitcoin.mainnet=true",
        RPCPort:        8332,
        P2PPort:        8333,
        ZMQBlockPort:   28332,
        ZMQTxPort:      28333,
        LNCLINetwork:   "mainnet",
        CookiePath:     ".cookie",
        DataSubdir:     "",
    }
}

// Testnet4 returns the testnet4 configuration.
func Testnet4() *NetworkConfig {
    return &NetworkConfig{
        Name:           "testnet4",
        BitcoinFlag:    "testnet4=1",
        LNDBitcoinFlag: "bitcoin.testnet4=true",
        RPCPort:        48332,
        P2PPort:        48333,
        ZMQBlockPort:   28334,
        ZMQTxPort:      28335,
        LNCLINetwork:   "testnet4",
        CookiePath:     "testnet4/.cookie",
        DataSubdir:     "testnet4",
    }
}

// NetworkConfigFromName returns the config for the given network name.
func NetworkConfigFromName(name string) *NetworkConfig {
    if name == "mainnet" {
        return Mainnet()
    }
    return Testnet4()
}