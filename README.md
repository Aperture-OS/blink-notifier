# Blink Notifier

A tiny self-hosted script that pings a Discord webhook whenever The Blink Repository has new packages to update.

## What it does

- Checks Blink-PM repo for updates.
- Sends a quick Discord message if something changed.
- Simple, minimal, and easy to run on a schedule through services.

## Usage

```bash
git clone https://github.com/Aperture-OS/blink-notifier.git
cd blink-notifier/src
go build main.go
./main # this runs a version check, make a service to run this whenever u want to do recurrent checks.
```

© Copyright Aperture OS 2025 Must Include the Copyright notice in any fork!
