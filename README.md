<p align="center">
  <img src="doc/spank-claw-logo.jpg" alt="spank-claw logo" width="300">
</p>

# spank-claw

**Your AI misbehaved. The claw has opinions.**

A fork of [taigrr/spank](https://github.com/taigrr/spank) that adds `--claude` mode. Same slap detection, same accelerometer wizardry, but now your laptop can type passive-aggressive prompts directly into your Claude Code session when you smack it out of frustration.

Because sometimes `Ctrl+C` doesn't convey enough emotion.

```bash
sudo spank-claw --claude --sexy
# your laptop now has feelings AND an escalation policy
```

> "I left it running and forgot. Typed a little too hard and Claude started apologizing for things it hadn't done yet." -- the first and only tester

## What `--claude` does

`--claude` is a modifier flag that stacks with any audio mode. When a slap is detected, it does two things:

1. Plays the audio response (ow, sexy moan, Halo death sound, whatever mode you picked)
2. Types a frustration-scaled prompt into your active terminal via macOS Accessibility

The harder and more frequently you slap, the angrier the prompt gets. 60 levels of escalation, capped at 35 for typed prompts (because "git stash drop" should require a keyboard, not a fist).

```bash
sudo spank-claw --claude                # pain sounds + angry prompts
sudo spank-claw --claude --sexy         # moans of escalating intensity + angry prompts
sudo spank-claw --claude --halo         # Master Chief dying + angry prompts
sudo spank-claw --claude --custom ~/wav # your sounds + angry prompts
```

### The escalation curve

Intensity builds over a rolling 5-minute window with exponential decay. One slap is a gentle nudge. Sustained slapping is a code review.

```
Light tap  (level 1-10):  "hmm, that's not quite what I meant"
Annoyed    (level 11-20): "I said the OTHER file"
Frustrated (level 21-30): "WHY IS THERE A NEW FILE I DIDN'T ASK FOR"
Furious    (level 31-35): "STOP. BREATHE. READ THE TASK. DO ONLY THE TASK."
                          (prompt injection caps here for safety)
Audio-only (level 36-60): sounds escalate but prompts stop
                          (some things should only be screamed, not typed)
```

Each prompt includes metadata so the AI knows exactly how angry you are:

```
<!-- frustration: 0.47g level: 23/60 -->
```

### Tuning

Claude mode ships with higher defaults than audio modes because **your typing registers as 0.05g impacts** and you do NOT want "hmm, that's not quite what I meant" injected every time you hit Enter.

```bash
# Defaults for --claude: 0.35g threshold, 1.2s cooldown
sudo spank-claw --claude

# More sensitive (risky on bouncy desks)
sudo spank-claw --claude --min-amplitude 0.2

# Less sensitive (only real anger)
sudo spank-claw --claude --min-amplitude 0.5

# Longer cooldown between prompts
sudo spank-claw --claude --cooldown 5000
```

**Calibration tips:**
- Put your laptop on a hard, flat surface (soft surfaces dampen impacts)
- Normal typing is ~0.05-0.08g. Default threshold of 0.35g ignores this.
- A real palm-slap is ~0.3-0.6g. An angry fist is 0.7g+.
- Moving the laptop sideways also triggers it (the accelerometer reads all axes)
- If prompts appear while you're just typing, raise `--min-amplitude`

### How it actually works

```
Accelerometer (IOKit HID, Bosch BMI286 IMU)
    |
    v
Impact detection (STA/LTA, CUSUM, kurtosis — seismology algorithms!)
    |
    v
Slap tracker (rolling 5-min window, 30s exponential decay half-life)
    |
    v
Score -> prompt level (1-exp(-x) curve, gentler for claude mode)
    |
    +---> Audio playback (embedded MP3, amplitude-scaled volume)
    |
    +---> osascript keystroke injection (macOS Accessibility API)
          |
          v
    Claude Code receives it as normal user input
    Claude has no idea it came from a slap
    Claude apologizes anyway
```

### Requirements

- macOS on Apple Silicon (M2+, or M1 Pro specifically)
- `sudo` (IOKit HID accelerometer needs root)
- Terminal app needs Accessibility permissions (System Preferences -> Privacy & Security -> Accessibility)
- Go 1.26+ (if building from source)

### Install

```bash
go install github.com/janzheng/spank-claw@latest
sudo cp "$(go env GOPATH)/bin/spank-claw" /usr/local/bin/spank-claw
```

Or clone and build:

```bash
git clone https://github.com/janzheng/spank-claw.git
cd spank-claw
go build -o spank-claw .
sudo ./spank-claw --claude
```

### The "frustration as metadata" hypothesis

There's an actually interesting idea buried in this joke: **physical frustration is a signal that text can't capture.** When you type "please try again," Claude doesn't know if you're mildly curious or silently fuming. But `<!-- frustration: 0.47g level: 23/60 -->` is unambiguous.

An agent that receives frustration metadata could:
- Be more conservative at high g-force (don't experiment, do exactly what was asked)
- Take more creative liberties at low g-force (user is calm, room to try things)
- Detect escalation patterns (3 slaps in 60 seconds = fundamentally wrong approach)

Is this a good idea? Absolutely not. Is it a better feedback mechanism than passive-aggressively rewriting your prompt for the fourth time? Maybe.

---

## All original spank features

Everything from [taigrr/spank](https://github.com/taigrr/spank) works unchanged. `--claude` just adds prompt injection on top.

# spank (original)

**English** | [简体中文][readme-zh-link]

Slap your MacBook, it yells back.

> "this is the most amazing thing i've ever seen" — [@kenwheeler](https://x.com/kenwheeler)

> "I just ran sexy mode with my wife sitting next to me...We died laughing" — [@duncanthedev](https://x.com/duncanthedev)

> "peak engineering" — [@tylertaewook](https://x.com/tylertaewook)

Uses the Apple Silicon accelerometer (Bosch BMI286 IMU via IOKit HID) to detect physical hits on your laptop and plays audio responses. Single binary, no dependencies.

## Requirements

- macOS on Apple Silicon (any M-series chip M2 or greater, or the M1 Pro SKU specifically, no other M1/A-series chips!)
- `sudo` (for IOKit HID accelerometer access)
- Go 1.26+ (if building from source)

## Install

Download from the [latest release](https://github.com/taigrr/spank/releases/latest).

Or build from source:

```bash
go install github.com/taigrr/spank@latest
```

> **Note:** `go install` places the binary in `$GOBIN` (if set) or `$(go env GOPATH)/bin` (which defaults to `~/go/bin`). Copy it to a system path so `sudo spank` works. For example, with the default Go settings:
>
> ```bash
> sudo cp "$(go env GOPATH)/bin/spank" /usr/local/bin/spank
> ```

## Usage

```bash
# Normal mode — says "ow!" when slapped
sudo spank

# Sexy mode — escalating responses based on slap frequency
sudo spank --sexy

# Halo mode — plays Halo death sounds when slapped
sudo spank --halo

# Fast mode — faster polling and shorter cooldown
sudo spank --fast
sudo spank --sexy --fast

# Custom mode — plays your own MP3 files from a directory
sudo spank --custom /path/to/mp3s

# Adjust sensitivity with amplitude threshold (lower = more sensitive)
sudo spank --min-amplitude 0.1   # more sensitive
sudo spank --min-amplitude 0.25  # less sensitive
sudo spank --sexy --min-amplitude 0.2

# Set cooldown period in millisecond (default: 750)
sudo spank --cooldown 600

# Set playback speed multiplier (default: 1.0)
sudo spank --speed 0.7   # slower and deeper
sudo spank --speed 1.5   # faster
sudo spank --sexy --speed 0.6
```

### Modes

**Pain mode** (default): Randomly plays from 10 pain/protest audio clips when a slap is detected.

**Sexy mode** (`--sexy`): Tracks slaps within a rolling 5-minute window. The more you slap, the more intense the audio response. 60 levels of escalation.

**Halo mode** (`--halo`): Randomly plays from death sound effects from the Halo video game series when a slap is detected.

**Custom mode** (`--custom`): Randomly plays MP3 files from a custom directory you specify.

### Detection tuning

Use `--fast` for a more responsive profile with faster polling (4ms vs 10ms), shorter cooldown (350ms vs 750ms), higher sensitivity (0.18 vs 0.05 threshold), and larger sample batch (320 vs 200).

You can still override individual values with `--min-amplitude` and `--cooldown` when needed.

### Sensitivity

Control detection sensitivity with `--min-amplitude` (default: `0.05`):

- Lower values (e.g., 0.05-0.10): Very sensitive, detects light taps
- Medium values (e.g., 0.15-0.30): Balanced sensitivity
- Higher values (e.g., 0.30-0.50): Only strong impacts trigger sounds

The value represents the minimum acceleration amplitude (in g-force) required to trigger a sound.

## Running as a Service

To have spank start automatically at boot, create a launchd plist. Pick your mode:

<details>
<summary>Pain mode (default)</summary>

```bash
sudo tee /Library/LaunchDaemons/com.taigrr.spank.plist > /dev/null << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.taigrr.spank</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/spank</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/spank.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/spank.err</string>
</dict>
</plist>
EOF
```

</details>

<details>
<summary>Sexy mode</summary>

```bash
sudo tee /Library/LaunchDaemons/com.taigrr.spank.plist > /dev/null << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.taigrr.spank</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/spank</string>
        <string>--sexy</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/spank.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/spank.err</string>
</dict>
</plist>
EOF
```

</details>

<details>
<summary>Halo mode</summary>

```bash
sudo tee /Library/LaunchDaemons/com.taigrr.spank.plist > /dev/null << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.taigrr.spank</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/spank</string>
        <string>--halo</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/spank.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/spank.err</string>
</dict>
</plist>
EOF
```

</details>

> **Note:** Update the path to `spank` if you installed it elsewhere (e.g. `~/go/bin/spank`).

Load and start the service:

```bash
sudo launchctl load /Library/LaunchDaemons/com.taigrr.spank.plist
```

Since the plist lives in `/Library/LaunchDaemons` and no `UserName` key is set, launchd runs it as root — no `sudo` needed.

To stop or unload:

```bash
sudo launchctl unload /Library/LaunchDaemons/com.taigrr.spank.plist
```

## How it works

1. Reads raw accelerometer data directly via IOKit HID (Apple SPU sensor)
2. Runs vibration detection (STA/LTA, CUSUM, kurtosis, peak/MAD)
3. When a significant impact is detected, plays an embedded MP3 response
4. **Optional volume scaling** (`--volume-scaling`) — light taps play quietly, hard slaps play at full volume
5. **Optional speed control** (`--speed`) — adjusts playback speed and pitch (0.5 = half speed, 2.0 = double speed)
6. 750ms cooldown between responses to prevent rapid-fire, adjustable with `--cooldown`

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=taigrr/spank&type=date&legend=top-left)](https://www.star-history.com/#taigrr/spank&type=date&legend=top-left)

## Credits

Sensor reading and vibration detection ported from [olvvier/apple-silicon-accelerometer](https://github.com/olvvier/apple-silicon-accelerometer).

## License

MIT

<!-- Links -->
[readme-zh-link]: ./README-zh.md
