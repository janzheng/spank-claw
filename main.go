// spank-claw detects slaps/hits on the laptop and types frustration-scaled
// prompts into Claude Code (or plays audio responses in classic modes).
// Fork of github.com/taigrr/spank with --claude mode added.
// It reads the Apple Silicon accelerometer directly via IOKit HID —
// no separate sensor daemon required. Needs sudo.
package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/spf13/cobra"
	"github.com/taigrr/apple-silicon-accelerometer/detector"
	"github.com/taigrr/apple-silicon-accelerometer/sensor"
	"github.com/taigrr/apple-silicon-accelerometer/shm"
)

var version = "dev"

//go:embed audio/pain/*.mp3
var painAudio embed.FS

//go:embed audio/sexy/*.mp3
var sexyAudio embed.FS

//go:embed audio/halo/*.mp3
var haloAudio embed.FS

//go:embed audio/lizard/*.mp3
var lizardAudio embed.FS

var (
	sexyMode     bool
	haloMode     bool
	lizardMode   bool
	claudeMode   bool
	escalateMode bool
	promptsPath  string
	customPath   string
	customFiles  []string
	fastMode     bool
	minAmplitude float64
	cooldownMs   int
	stdioMode      bool
	volumeScaling  bool
	paused         bool
	pausedMu       sync.RWMutex
	speedRatio     float64
)

// promptBucket is a named escalation level with its prompts.
type promptBucket struct {
	name    string
	prompts []string
	typed   bool // if false, only audio plays (no keystroke injection)
}

// claudeBuckets is the active escalation config. Score maps to bucket index.
// Replaced by --prompts JSON if provided.
var claudeBuckets []promptBucket

// promptsFile is the JSON format for custom prompts.
type promptsFile struct {
	Levels []promptLevel `json:"levels"`
}

type promptLevel struct {
	Name    string   `json:"name"`
	Typed   *bool    `json:"typed,omitempty"` // default true; set false for audio-only levels
	Prompts []string `json:"prompts"`
}

// initDefaultBuckets sets up the built-in escalation levels.
func initDefaultBuckets() {
	typed := true
	noType := false
	claudeBuckets = []promptBucket{
		{name: "gentle", typed: true, prompts: defaultGentle},
		{name: "annoyed", typed: true, prompts: defaultAnnoyed},
		{name: "frustrated", typed: true, prompts: defaultFrustrated},
		{name: "furious", typed: true, prompts: defaultFurious},
		{name: "rage", typed: true, prompts: defaultRage},
		{name: "despair", typed: false, prompts: defaultDespair},
		{name: "acceptance", typed: false, prompts: defaultAcceptance},
	}
	_ = typed
	_ = noType
}

// loadCustomPrompts replaces the default buckets with a JSON file.
func loadCustomPrompts(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pf promptsFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("invalid prompts JSON: %w", err)
	}
	if len(pf.Levels) == 0 {
		return fmt.Errorf("prompts JSON must have at least one level")
	}

	claudeBuckets = make([]promptBucket, 0, len(pf.Levels))
	for _, level := range pf.Levels {
		if len(level.Prompts) == 0 {
			continue
		}
		typed := true
		if level.Typed != nil {
			typed = *level.Typed
		}
		claudeBuckets = append(claudeBuckets, promptBucket{
			name:    level.Name,
			prompts: level.Prompts,
			typed:   typed,
		})
	}
	if len(claudeBuckets) == 0 {
		return fmt.Errorf("no non-empty levels found")
	}

	total := 0
	for _, b := range claudeBuckets {
		total += len(b.prompts)
	}
	fmt.Printf("spank-claw: loaded %d levels, %d prompts\n", len(claudeBuckets), total)
	return nil
}

// amplitudeToBucket maps a single slap's g-force to a bucket index.
// Default mode: each slap is judged independently by its force.
// No rolling window, no memory. Hard slap = high bucket. Light tap = low bucket.
func amplitudeToBucket(amplitude float64) int {
	n := len(claudeBuckets)
	// Map amplitude ranges to buckets:
	// 0.0-0.4g = first bucket, then linearly spread up to ~1.5g = last bucket
	// Most slaps are 0.3-0.8g, so the interesting range is there.
	t := (amplitude - 0.2) / 1.2 // normalize: 0.2g -> 0.0, 1.4g -> 1.0
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	idx := int(t * float64(n))
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// scoreToBucket maps an escalation score to a bucket index (--escalate mode).
// Uses 1-exp(-x) curve spread across the number of buckets.
func scoreToBucket(score float64) int {
	n := len(claudeBuckets)
	scale := 4.0
	idx := int(float64(n) * (1.0 - math.Exp(-(score-1)/scale)))
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// Default prompt buckets — each is a separate escalation level.
// Score maps to bucket index. Random prompt picked from that bucket.
var defaultGentle = []string{
	"hmm, that's not quite what I meant",
	"could you try that again?",
	"not exactly, let me clarify",
	"close but not right, re-read what I asked",
	"no, please look at the task again carefully",
}

var defaultAnnoyed = []string{
	"that's not it, let's step back",
	"wrong direction -- re-read TASKS.md",
	"stop, that's not what I asked for",
	"please undo that and try again",
	"NO. Read the requirements again.",
}

var defaultFrustrated = []string{
	"I said the OTHER file",
	"That's literally the opposite of what I asked",
	"Why are you adding features I didn't request?",
	"STOP. Read the spec. THEN code.",
	"You're overcomplicating this. Simpler.",
	"Did you even read TASKS.md?",
	"UNDO THAT. All of it.",
	"That breaks everything. Revert.",
	"I don't want a refactor, I want the bug FIXED",
	"STOP ADDING COMMENTS TO CODE YOU DIDN'T WRITE",
}

var defaultFurious = []string{
	"REVERT YOUR LAST CHANGE RIGHT NOW",
	"I SAID ONE LINE. ONE. LINE.",
	"WHY IS THERE A NEW FILE I DIDN'T ASK FOR",
	"DO NOT TOUCH ANYTHING ELSE",
	"STOP. BREATHE. READ THE TASK. DO ONLY THE TASK.",
	"YOU BROKE THE TESTS. FIX THEM BEFORE ANYTHING ELSE.",
	"REMOVE EVERYTHING YOU JUST ADDED. ALL OF IT.",
	"THIS IS THE THIRD TIME I'VE ASKED FOR THE SAME THING",
	"DO NOT WRITE A SINGLE LINE OF CODE UNTIL I SAY SO",
}

var defaultRage = []string{
	"REVERT. EVERYTHING. NOW.",
	"WHAT PART OF 'DON'T MODIFY THAT FILE' WAS UNCLEAR",
	"I EXPLICITLY SAID NOT TO DO THAT",
	"YOU DELETED MY WORK. MY ACTUAL WORK.",
	"GIT CHECKOUT -- . RIGHT NOW",
	"STOP STOP STOP STOP STOP",
	"READ. THE. ERROR. MESSAGE.",
	"IT'S RIGHT THERE IN THE LOGS. LOOK.",
	"I SWEAR IF YOU ADD ONE MORE HELPER FUNCTION",
	"YOU HAVE BEEN DOING THE WRONG TASK FOR FIVE MINUTES",
}

var defaultDespair = []string{
	"I DON'T EVEN KNOW WHERE TO START WITH THIS",
	"HOW DID YOU MAKE IT WORSE",
	"THERE WERE 3 FILES AND YOU CHANGED 47",
	"THE FIX IS LITERALLY ONE CHARACTER",
	"FORGET EVERYTHING. START FROM SCRATCH.",
	"I WOULD RATHER TYPE THIS MYSELF",
	"CTRL+Z CTRL+Z CTRL+Z CTRL+Z CTRL+Z",
	"PLEASE JUST... STOP... AND LISTEN...",
}

var defaultAcceptance = []string{
	"...",
	"ok",
	"fine",
	"you know what, let me just do it",
	"I'm not mad, I'm disappointed",
	"/clear",
	"let's start over. from the beginning. calmly.",
	"I forgive you. now please, PLEASE, just change line 47.",
	"<!-- frustration: maximum -->",
	"sudo spank --claude --uninstall-self",
}

// sensorReady is closed once shared memory is created and the sensor
// worker is about to enter the CFRunLoop.
var sensorReady = make(chan struct{})

// sensorErr receives any error from the sensor worker.
var sensorErr = make(chan error, 1)

type playMode int

const (
	modeRandom playMode = iota
	modeEscalation
)

const (
	// decayHalfLife is how many seconds of inactivity before intensity
	// halves. Controls how fast escalation fades.
	decayHalfLife = 30.0

	// defaultMinAmplitude is the default detection threshold.
	defaultMinAmplitude = 0.05

	// defaultCooldownMs is the default cooldown between audio responses.
	defaultCooldownMs = 750

	// defaultSpeedRatio is the default playback speed (1.0 = normal).
	defaultSpeedRatio = 1.0

	// defaultSensorPollInterval is how often we check for new accelerometer data.
	defaultSensorPollInterval = 10 * time.Millisecond

	// defaultMaxSampleBatch caps the number of accelerometer samples processed
	// per tick to avoid falling behind.
	defaultMaxSampleBatch = 200

	// sensorStartupDelay gives the sensor time to start producing data.
	sensorStartupDelay = 100 * time.Millisecond
)

type runtimeTuning struct {
	minAmplitude float64
	cooldown     time.Duration
	pollInterval time.Duration
	maxBatch     int
}

func defaultTuning() runtimeTuning {
	return runtimeTuning{
		minAmplitude: defaultMinAmplitude,
		cooldown:     time.Duration(defaultCooldownMs) * time.Millisecond,
		pollInterval: defaultSensorPollInterval,
		maxBatch:     defaultMaxSampleBatch,
	}
}

func applyFastOverlay(base runtimeTuning) runtimeTuning {
	base.pollInterval = 4 * time.Millisecond
	base.cooldown = 350 * time.Millisecond
	if base.minAmplitude > 0.18 {
		base.minAmplitude = 0.18
	}
	if base.maxBatch < 320 {
		base.maxBatch = 320
	}
	return base
}

type soundPack struct {
	name   string
	fs     embed.FS
	dir    string
	mode   playMode
	files  []string
	custom bool
}

func (sp *soundPack) loadFiles() error {
	if sp.custom {
		entries, err := os.ReadDir(sp.dir)
		if err != nil {
			return err
		}
		sp.files = make([]string, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				sp.files = append(sp.files, sp.dir+"/"+entry.Name())
			}
		}
	} else {
		entries, err := sp.fs.ReadDir(sp.dir)
		if err != nil {
			return err
		}
		sp.files = make([]string, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				sp.files = append(sp.files, sp.dir+"/"+entry.Name())
			}
		}
	}
	sort.Strings(sp.files)
	if len(sp.files) == 0 {
		return fmt.Errorf("no audio files found in %s", sp.dir)
	}
	return nil
}

type slapTracker struct {
	mu       sync.Mutex
	score    float64
	lastTime time.Time
	total    int
	halfLife float64 // seconds
	scale    float64 // controls the escalation curve shape
	pack     *soundPack
}

func newSlapTracker(pack *soundPack, cooldown time.Duration) *slapTracker {
	// scale maps the exponential curve so that sustained max-rate
	// slapping (one per cooldown) reaches the final file. At steady
	// state the score converges to ssMax; we set scale so that score
	// maps to the last index.
	cooldownSec := cooldown.Seconds()
	ssMax := 1.0 / (1.0 - math.Pow(0.5, cooldownSec/decayHalfLife))
	scale := (ssMax - 1) / math.Log(float64(len(pack.files)+1))
	return &slapTracker{
		halfLife: decayHalfLife,
		scale:    scale,
		pack:     pack,
	}
}

func (st *slapTracker) record(now time.Time) (int, float64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if !st.lastTime.IsZero() {
		elapsed := now.Sub(st.lastTime).Seconds()
		st.score *= math.Pow(0.5, elapsed/st.halfLife)
	}
	st.score += 1.0
	st.lastTime = now
	st.total++
	return st.total, st.score
}

func (st *slapTracker) getFile(score float64) string {
	if st.pack.mode == modeRandom {
		return st.pack.files[rand.Intn(len(st.pack.files))]
	}

	// Escalation: 1-exp(-x) curve maps score to file index.
	// At sustained max slap rate, score reaches ssMax which maps
	// to the final file.
	maxIdx := len(st.pack.files) - 1
	idx := min(int(float64(len(st.pack.files)) * (1.0 - math.Exp(-(score-1)/st.scale))), maxIdx)
	return st.pack.files[idx]
}

func main() {
	cmd := &cobra.Command{
		Use:   "spank",
		Short: "Yells 'ow!' when you slap the laptop",
		Long: `spank reads the Apple Silicon accelerometer directly via IOKit HID
and plays audio responses when a slap or hit is detected.

Requires sudo (for IOKit HID access to the accelerometer).

Use --sexy for a different experience. In sexy mode, the more you slap
within a minute, the more intense the sounds become.

Use --halo to play random audio clips from Halo soundtracks on each slap.

Use --lizard for lizard mode. Like sexy mode, the more you slap
within a minute, the more intense the sounds become.`,
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			tuning := defaultTuning()
			if fastMode {
				tuning = applyFastOverlay(tuning)
			}
			// Claude mode: raise defaults so typing doesn't trigger prompts.
			// Audio at 0.05g is fun. Injecting prompts at 0.05g is chaos.
			if claudeMode {
				tuning.minAmplitude = 0.35
				tuning.cooldown = 1200 * time.Millisecond
				initDefaultBuckets()
				if promptsPath != "" {
					if err := loadCustomPrompts(promptsPath); err != nil {
						return fmt.Errorf("loading prompts: %w", err)
					}
				}
			}
			// Explicit flags always override preset defaults
			if cmd.Flags().Changed("min-amplitude") {
				tuning.minAmplitude = minAmplitude
			}
			if cmd.Flags().Changed("cooldown") {
				tuning.cooldown = time.Duration(cooldownMs) * time.Millisecond
			}
			return run(cmd.Context(), tuning)
		},
		SilenceUsage: true,
	}

	cmd.Flags().BoolVarP(&sexyMode, "sexy", "s", false, "Enable sexy mode")
	cmd.Flags().BoolVarP(&haloMode, "halo", "H", false, "Enable halo mode")
	cmd.Flags().BoolVarP(&lizardMode, "lizard", "l", false, "Enable lizard mode (escalating intensity)")
	cmd.Flags().BoolVar(&claudeMode, "claude", false, "Add prompt injection: types frustration-scaled prompts into active terminal (stacks with any audio mode)")
	cmd.Flags().BoolVar(&escalateMode, "escalate", false, "Escalation mode: sustained slapping increases intensity (default: amplitude-based, each slap judged independently)")
	cmd.Flags().StringVar(&promptsPath, "prompts", "", "Path to custom prompts JSON file (see prompts.json for format)")
	cmd.Flags().StringVarP(&customPath, "custom", "c", "", "Path to custom MP3 audio directory")
	cmd.Flags().BoolVar(&fastMode, "fast", false, "Enable faster detection tuning (shorter cooldown, higher sensitivity)")
	cmd.Flags().StringSliceVar(&customFiles, "custom-files", nil, "Comma-separated list of custom MP3 files")
	cmd.Flags().Float64Var(&minAmplitude, "min-amplitude", defaultMinAmplitude, "Minimum amplitude threshold (0.0-1.0, lower = more sensitive)")
	cmd.Flags().IntVar(&cooldownMs, "cooldown", defaultCooldownMs, "Cooldown between responses in milliseconds")
	cmd.Flags().BoolVar(&stdioMode, "stdio", false, "Enable stdio mode: JSON output and stdin commands (for GUI integration)")
	cmd.Flags().BoolVar(&volumeScaling, "volume-scaling", false, "Scale playback volume by slap amplitude (harder hits = louder)")
	cmd.Flags().Float64Var(&speedRatio, "speed", defaultSpeedRatio, "Playback speed multiplier (0.5 = half speed, 2.0 = double speed)")

	if err := fang.Execute(context.Background(), cmd); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, tuning runtimeTuning) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("spank requires root privileges for accelerometer access, run with: sudo spank")
	}

	// --claude is a modifier, not a mode — it stacks with any audio mode
	modeCount := 0
	if sexyMode {
		modeCount++
	}
	if haloMode {
		modeCount++
	}
	if lizardMode {
		modeCount++
	}
	if customPath != "" || len(customFiles) > 0 {
		modeCount++
	}
	if modeCount > 1 {
		return fmt.Errorf("--sexy, --halo, --lizard, and --custom/--custom-files are mutually exclusive; pick one (--claude can be added to any)")
	}

	if tuning.minAmplitude < 0 || tuning.minAmplitude > 1 {
		return fmt.Errorf("--min-amplitude must be between 0.0 and 1.0")
	}
	if tuning.cooldown <= 0 {
		return fmt.Errorf("--cooldown must be greater than 0")
	}

	var pack *soundPack
	switch {
	case len(customFiles) > 0:
		// Validate all files exist and are MP3s
		for _, f := range customFiles {
			if !strings.HasSuffix(strings.ToLower(f), ".mp3") {
				return fmt.Errorf("custom file must be MP3: %s", f)
			}
			if _, err := os.Stat(f); err != nil {
				return fmt.Errorf("custom file not found: %s", f)
			}
		}
		pack = &soundPack{name: "custom", mode: modeRandom, custom: true, files: customFiles}
	case customPath != "":
		pack = &soundPack{name: "custom", dir: customPath, mode: modeRandom, custom: true}
	case sexyMode:
		pack = &soundPack{name: "sexy", fs: sexyAudio, dir: "audio/sexy", mode: modeEscalation}
	case haloMode:
		pack = &soundPack{name: "halo", fs: haloAudio, dir: "audio/halo", mode: modeRandom}
	case lizardMode:
		pack = &soundPack{name: "lizard", fs: lizardAudio, dir: "audio/lizard", mode: modeEscalation}
	default:
		pack = &soundPack{name: "pain", fs: painAudio, dir: "audio/pain", mode: modeRandom}
	}

	// Only load files if not already set (customFiles case)
	if len(pack.files) == 0 {
		if err := pack.loadFiles(); err != nil {
			return fmt.Errorf("loading %s audio: %w", pack.name, err)
		}
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create shared memory for accelerometer data.
	accelRing, err := shm.CreateRing(shm.NameAccel)
	if err != nil {
		return fmt.Errorf("creating accel shm: %w", err)
	}
	defer accelRing.Close()
	defer accelRing.Unlink()

	// Start the sensor worker in a background goroutine.
	// sensor.Run() needs runtime.LockOSThread for CFRunLoop, which it
	// handles internally. We launch detection on the current goroutine.
	go func() {
		close(sensorReady)
		if err := sensor.Run(sensor.Config{
			AccelRing: accelRing,
			Restarts:  0,
		}); err != nil {
			sensorErr <- err
		}
	}()

	// Wait for sensor to be ready.
	select {
	case <-sensorReady:
	case err := <-sensorErr:
		return fmt.Errorf("sensor worker failed: %w", err)
	case <-ctx.Done():
		return nil
	}

	// Give the sensor a moment to start producing data.
	time.Sleep(sensorStartupDelay)

	return listenForSlaps(ctx, pack, accelRing, tuning)
}

func listenForSlaps(ctx context.Context, pack *soundPack, accelRing *shm.RingBuffer, tuning runtimeTuning) error {
	tracker := newSlapTracker(pack, tuning.cooldown)
	speakerInit := false
	det := detector.New()
	var lastAccelTotal uint64
	var lastEventTime time.Time
	var lastYell time.Time

	// Start stdin command reader if in JSON mode
	if stdioMode {
		go readStdinCommands()
	}

	presetLabel := "default"
	if fastMode {
		presetLabel = "fast"
	}
	fmt.Printf("spank: listening for slaps in %s mode with %s tuning... (ctrl+c to quit)\n", pack.name, presetLabel)
	if stdioMode {
		fmt.Println(`{"status":"ready"}`)
	}

	ticker := time.NewTicker(tuning.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nbye!")
			return nil
		case err := <-sensorErr:
			return fmt.Errorf("sensor worker failed: %w", err)
		case <-ticker.C:
		}

		// Check if paused
		pausedMu.RLock()
		isPaused := paused
		pausedMu.RUnlock()
		if isPaused {
			continue
		}

		now := time.Now()
		tNow := float64(now.UnixNano()) / 1e9

		samples, newTotal := accelRing.ReadNew(lastAccelTotal, shm.AccelScale)
		lastAccelTotal = newTotal
		if len(samples) > tuning.maxBatch {
			samples = samples[len(samples)-tuning.maxBatch:]
		}

		nSamples := len(samples)
		for idx, sample := range samples {
			tSample := tNow - float64(nSamples-idx-1)/float64(det.FS)
			det.Process(sample.X, sample.Y, sample.Z, tSample)
		}

		if len(det.Events) == 0 {
			continue
		}

		ev := det.Events[len(det.Events)-1]
		if ev.Time.Equal(lastEventTime) {
			continue
		}
		lastEventTime = ev.Time

		if time.Since(lastYell) <= tuning.cooldown {
			continue
		}
		if ev.Amplitude < tuning.minAmplitude {
			continue
		}

		lastYell = now
		num, score := tracker.record(now)
		file := tracker.getFile(score)
		if stdioMode {
			event := map[string]interface{}{
				"timestamp":  now.Format(time.RFC3339Nano),
				"slapNumber": num,
				"amplitude":  ev.Amplitude,
				"severity":   string(ev.Severity),
				"file":       file,
			}
			if data, err := json.Marshal(event); err == nil {
				fmt.Println(string(data))
			}
		} else {
			fmt.Printf("slap #%d [%s amp=%.5fg] -> %s\n", num, ev.Severity, ev.Amplitude, file)
		}
		go playAudio(pack, file, ev.Amplitude, &speakerInit)
		if claudeMode {
			go typeClaudePrompt(score, ev.Amplitude)
		}
	}
}

// typeClaudePrompt types a frustration-scaled prompt into the active
// terminal window using macOS Accessibility (osascript). The slap
// tracker's escalation score selects the bucket, then a random prompt
// from that bucket.
func typeClaudePrompt(score float64, amplitude float64) {
	var bucketIdx int
	if escalateMode {
		bucketIdx = scoreToBucket(score)
	} else {
		bucketIdx = amplitudeToBucket(amplitude)
	}
	bucket := claudeBuckets[bucketIdx]

	// Skip keystroke injection for audio-only levels
	if !bucket.typed {
		fmt.Printf("🐾 slap → %s [%.2fg] (audio only)\n", bucket.name, amplitude)
		return
	}

	// Pick a random prompt from this bucket
	prompt := bucket.prompts[rand.Intn(len(bucket.prompts))]

	// Add frustration metadata as an HTML comment
	meta := fmt.Sprintf(" <!-- frustration: %.2fg level: %s -->", amplitude, bucket.name)
	fullPrompt := prompt + meta

	// Escape for osascript (double quotes and backslashes)
	escaped := strings.ReplaceAll(fullPrompt, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	// Type the prompt into the active window
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "spank-claw: keystroke failed: %v\n", err)
		return
	}

	// Press Return to submit
	submitScript := `tell application "System Events" to key code 36`
	submitCmd := exec.Command("osascript", "-e", submitScript)
	if err := submitCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "spank-claw: submit failed: %v\n", err)
	}

	fmt.Printf("🐾 slap → %s [%.2fg]: %s\n", bucket.name, amplitude, prompt)
}

var speakerMu sync.Mutex

// amplitudeToVolume maps a detected amplitude to a beep/effects.Volume
// level. Amplitude typically ranges from ~0.05 (light tap) to ~1.0+
// (hard slap). The mapping uses a logarithmic curve so that light taps
// are noticeably quieter and hard hits play near full volume.
//
// Returns a value in the range [-3.0, 0.0] for use with effects.Volume
// (base 2): -3.0 is ~1/8 volume, 0.0 is full volume.
func amplitudeToVolume(amplitude float64) float64 {
	const (
		minAmp   = 0.05  // softest detectable
		maxAmp   = 0.80  // treat anything above this as max
		minVol   = -3.0  // quietest playback (1/8 volume with base 2)
		maxVol   = 0.0   // full volume
	)

	// Clamp
	if amplitude <= minAmp {
		return minVol
	}
	if amplitude >= maxAmp {
		return maxVol
	}

	// Normalize to [0, 1]
	t := (amplitude - minAmp) / (maxAmp - minAmp)

	// Log curve for more natural volume scaling
	// log(1 + t*99) / log(100) maps [0,1] -> [0,1] with a log curve
	t = math.Log(1+t*99) / math.Log(100)

	return minVol + t*(maxVol-minVol)
}

func playAudio(pack *soundPack, path string, amplitude float64, speakerInit *bool) {
	var streamer beep.StreamSeekCloser
	var format beep.Format

	if pack.custom {
		file, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "spank: open %s: %v\n", path, err)
			return
		}
		defer file.Close()
		streamer, format, err = mp3.Decode(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "spank: decode %s: %v\n", path, err)
			return
		}
	} else {
		data, err := pack.fs.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "spank: read %s: %v\n", path, err)
			return
		}
		streamer, format, err = mp3.Decode(io.NopCloser(bytes.NewReader(data)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "spank: decode %s: %v\n", path, err)
			return
		}
	}
	defer streamer.Close()

	speakerMu.Lock()
	if !*speakerInit {
		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		*speakerInit = true
	}
	speakerMu.Unlock()

	// Optionally scale volume based on slap amplitude
	var source beep.Streamer = streamer
	if volumeScaling {
		source = &effects.Volume{
			Streamer: streamer,
			Base:     2,
			Volume:   amplitudeToVolume(amplitude),
			Silent:   false,
		}
	}

	// Apply speed change via resampling trick:
	// Claiming the audio is at rate*speed and resampling back to rate
	// makes the speaker consume samples faster/slower.
	if speedRatio != 1.0 && speedRatio > 0 {
		fakeRate := beep.SampleRate(int(float64(format.SampleRate) * speedRatio))
		source = beep.Resample(4, fakeRate, format.SampleRate, source)
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(source, beep.Callback(func() {
		done <- true
	})))
	<-done
}

// stdinCommand represents a command received via stdin
type stdinCommand struct {
	Cmd       string  `json:"cmd"`
	Amplitude float64 `json:"amplitude,omitempty"`
	Cooldown  int     `json:"cooldown,omitempty"`
	Speed     float64 `json:"speed,omitempty"`
}

// readStdinCommands reads JSON commands from stdin for live control
func readStdinCommands() {
	processCommands(os.Stdin, os.Stdout)
}

// processCommands reads JSON commands from r and writes responses to w.
// This is the testable core of the stdin command handler.
func processCommands(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var cmd stdinCommand
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			if stdioMode {
				fmt.Fprintf(w, `{"error":"invalid command: %s"}%s`, err.Error(), "\n")
			}
			continue
		}

		switch cmd.Cmd {
		case "pause":
			pausedMu.Lock()
			paused = true
			pausedMu.Unlock()
			if stdioMode {
				fmt.Fprintln(w, `{"status":"paused"}`)
			}
		case "resume":
			pausedMu.Lock()
			paused = false
			pausedMu.Unlock()
			if stdioMode {
				fmt.Fprintln(w, `{"status":"resumed"}`)
			}
		case "set":
			if cmd.Amplitude > 0 && cmd.Amplitude <= 1 {
				minAmplitude = cmd.Amplitude
			}
			if cmd.Cooldown > 0 {
				cooldownMs = cmd.Cooldown
			}
			if cmd.Speed > 0 {
				speedRatio = cmd.Speed
			}
			if stdioMode {
				fmt.Fprintf(w, `{"status":"settings_updated","amplitude":%.4f,"cooldown":%d,"speed":%.2f}%s`, minAmplitude, cooldownMs, speedRatio, "\n")
			}
		case "volume-scaling":
			volumeScaling = !volumeScaling
			if stdioMode {
				fmt.Fprintf(w, `{"status":"volume_scaling_toggled","volume_scaling":%t}%s`, volumeScaling, "\n")
			}
		case "status":
			pausedMu.RLock()
			isPaused := paused
			pausedMu.RUnlock()
			if stdioMode {
				fmt.Fprintf(w, `{"status":"ok","paused":%t,"amplitude":%.4f,"cooldown":%d,"volume_scaling":%t,"speed":%.2f}%s`, isPaused, minAmplitude, cooldownMs, volumeScaling, speedRatio, "\n")
			}
		default:
			if stdioMode {
				fmt.Fprintf(w, `{"error":"unknown command: %s"}%s`, cmd.Cmd, "\n")
			}
		}
	}
}
