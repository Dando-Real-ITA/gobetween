package healthcheck

/**
 * ping.go - TCP ping healthcheck
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"net"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/utils/proxyprotocol"
)

/**
 * Ping healthcheck
 */
func ping(t core.Target, cfg config.HealthcheckConfig, result chan<- CheckResult) {

	pingTimeoutDuration, _ := time.ParseDuration(cfg.Timeout)

	log := logging.For("healthcheck/ping")

	checkResult := CheckResult{
		Target: t,
	}

	conn, err := net.DialTimeout("tcp", t.Address(), pingTimeoutDuration)
	if err != nil {
		checkResult.Status = Unhealthy
	} else {
		defer conn.Close()
		if cfg.ProxyProtocol != nil {
			switch cfg.ProxyProtocol.Version {
			case "1":
				err = proxyprotocol.SendProxyProtocolV1Addrs(conn.LocalAddr(), conn.RemoteAddr(), conn)
				if err != nil {
					log.Debugf("Could not send proxy protocol header: %v", err)
					checkResult.Status = Unhealthy
					break
				}
				checkResult.Status = Healthy
			default:
				log.Debugf("Unsupported proxy protocol version for ping healthcheck: %s", cfg.ProxyProtocol.Version)
				checkResult.Status = Unhealthy
			}
		} else {
			checkResult.Status = Healthy
		}
	}

	select {
	case result <- checkResult:
	default:
		log.Warn("Channel is full. Discarding value")
	}
}
