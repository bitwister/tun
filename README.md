# Go VPN client for XRay
![Static Badge](https://img.shields.io/badge/OS-macOS%20%7C%20Linux-blue?style=flat&logo=linux&logoColor=white&logoSize=auto&color=blue)
![Static Badge](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white)

This project brings fully functioning [XRay](https://github.com/XTLS/Xray-core) VPN client implementation in Go.
<p align="center">
<img alt="Terminal example output" width="80%" align="center" src="/.github/images/carbon.svg">
</p>

> [!NOTE]
> The program will not damage your routing rules, default route is intact and only additional rules are added for the lifetime of application's TUN device. There are also additional complementary clean up procedures in place.

#### What is XRay?
Please visit https://xtls.github.io/en for more info.

#### Tested and supported on:
- macOS (tested on Sequoia 15.1.1)
- Linux (tested on Ubuntu 24.10)

> Feel free to test this on your system and let me know in the issues :)

## ⚡️ Usage
> [!IMPORTANT]
> - `sudo` is required
> - CGO_ENABLED=1 is required in order to build the project

Running the VPN on your machine is as simple as running this little command:

```bash
sudo go run . <proto_link>
```

Where `proto_link` is your XRay link (like `vless://example.com...`), you can get this from your VPN provider or get it from your XRay server.
## ✨ Features
- Stupidly easy to use
- Supports all [Xray-core](https://github.com/XTLS/Xray-core) protocols (vless, vmess e.t.c.) using link notation (`vless://` e.t.c.)
- Only soft routing rules are applied, no changes made to default routes

## 📝 TODO
- [ ] Add IPV6 support

## How it works
- Application sets up new TUN device.
- Adds additional routes to route all system traffic to this newly created TUN device.
- Adds exception for XRay outbound address (basically your VPN server IP).
- Tunnel is created to process all incoming IP packets via TCP/IP stack. All outbound traffic is routed through the XRay inbound proxy and all incoming packets are routed back via TUN device.

## 🎯 Motivation
There are no available XRay clients implementations in Go on Github, so I decided to do it myself. The attempt proved to be successfull and I wanted to share my findings in a complete and working VPN client.
