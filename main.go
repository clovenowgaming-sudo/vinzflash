package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var in = bufio.NewReader(os.Stdin)

func ask(p string) string {
	fmt.Print(p)
	s, _ := in.ReadString('\n')
	s = strings.TrimSpace(s)
	return strings.Trim(s, "\"")
}

func confirm(msg string) bool {
	return strings.ToUpper(ask(msg+" Ketik YA untuk lanjut: ")) == "YA"
}

func human(n int64) string {
	u := []string{"B", "KB", "MB", "GB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, u[i])
}

// ---------- jalankan adb / fastboot ----------

func findTool(name string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	var dirs []string
	if self, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(self))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, d := range dirs {
		p := filepath.Join(d, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		p = filepath.Join(d, "platform-tools", name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func run(name string, args ...string) {
	t := findTool(name)
	if t == "" {
		fmt.Printf("ERROR: %s tidak ditemukan. Taruh %s di folder yang sama dengan program ini (atau folder platform-tools / PATH).\n", name, name)
		return
	}
	fmt.Printf("\n$ %s %s\n", name, strings.Join(args, " "))
	c := exec.Command(t, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		fmt.Println("Gagal:", err)
	}
}

// ---------- lihat file ----------

func listDir() {
	d := ask("Folder (kosong = folder sekarang): ")
	if d == "" {
		d = "."
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	abs, _ := filepath.Abs(d)
	fmt.Println("\n" + abs)
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if e.IsDir() {
			fmt.Printf("  [DIR]       %s\n", e.Name())
		} else {
			fmt.Printf("  %-10s  %s\n", human(info.Size()), e.Name())
		}
	}
}

func fileInfo() {
	p := ask("Path file: ")
	f, err := os.Open(p)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	head := make([]byte, 64)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	f.Seek(0, 0)
	h := sha256.New()
	io.Copy(h, f)
	fmt.Printf("Ukuran : %d byte (%s)\nSHA256 : %x\nHeader : %s\n", st.Size(), human(st.Size()), h.Sum(nil), hex.EncodeToString(head))
	if bytes.HasPrefix(head, []byte("ANDROID!")) {
		fmt.Println("Tipe   : Android boot image (lihat menu Patch untuk detail)")
	} else if bytes.HasPrefix(head, []byte{0x3a, 0xff, 0x26, 0xed}) {
		fmt.Println("Tipe   : Android sparse image")
	}
}

// ---------- fastboot ----------

func checkDevices() {
	run("adb", "devices")
	run("fastboot", "devices")
}

func enterFastboot() {
	fmt.Println("1. Bootloader (fastboot biasa)  2. fastbootd (userspace)")
	switch ask("Pilih: ") {
	case "2":
		run("adb", "reboot", "fastboot")
	default:
		run("adb", "reboot", "bootloader")
	}
	fmt.Println("Kalau HP sudah di fastboot dan adb tidak mendeteksi, pakai menu Fastboot lainnya -> reboot-bootloader.")
}

func flashFile() {
	part := ask("Partisi (mis. boot, recovery, vendor_boot, dtbo, vbmeta) [boot]: ")
	if part == "" {
		part = "boot"
	}
	file := ask("Path file image: ")
	if _, err := os.Stat(file); err != nil {
		fmt.Println("Error:", err)
		return
	}
	slot := ask("Slot (a / b / all / kosong): ")
	args := []string{}
	if slot != "" {
		args = append(args, "--slot="+slot)
	}
	if strings.HasPrefix(part, "vbmeta") && strings.ToUpper(ask("Disable verity & verification? (y/n): ")) == "Y" {
		args = append(args, "--disable-verity", "--disable-verification")
	}
	args = append(args, "flash", part, file)
	if confirm(fmt.Sprintf("Flash %s ke partisi '%s'. Salah flash bisa bootloop/brick.", filepath.Base(file), part)) {
		run("fastboot", args...)
	}
}

func fastbootMore() {
	fmt.Println(`
a. Boot image sementara (fastboot boot)
b. getvar all
c. reboot
d. reboot-bootloader
e. reboot recovery
f. flashing unlock  (WIPE DATA)
g. flashing lock    (WIPE DATA)`)
	switch strings.ToLower(ask("Pilih: ")) {
	case "a":
		f := ask("Path image: ")
		run("fastboot", "boot", f)
	case "b":
		run("fastboot", "getvar", "all")
	case "c":
		run("fastboot", "reboot")
	case "d":
		run("fastboot", "reboot", "bootloader")
	case "e":
		run("fastboot", "reboot", "recovery")
	case "f":
		if confirm("Unlock bootloader akan MENGHAPUS semua data.") {
			run("fastboot", "flashing", "unlock")
		}
	case "g":
		if confirm("Lock bootloader dengan image non-stock bisa brick & WIPE DATA.") {
			run("fastboot", "flashing", "lock")
		}
	}
}

// ---------- boot.img ----------

type Boot struct {
	Hdr                                       []byte
	Ver                                       uint32
	Page                                      uint32
	Kernel, Ramdisk, Second, Dtbo, Dtb, Sig []byte
}

func le32(b []byte) uint32     { return binary.LittleEndian.Uint32(b) }
func put32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func align(n, p uint32) uint32 { return (n + p - 1) / p * p }

func cstr(b []byte) string {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		i = len(b)
	}
	return string(b[:i])
}

func parseBoot(d []byte) (*Boot, error) {
	if len(d) < 4096 || string(d[:8]) != "ANDROID!" {
		return nil, errors.New("bukan boot image Android (magic ANDROID! tidak ada)")
	}
	b := &Boot{Ver: le32(d[40:])}
	if b.Ver > 4 {
		return nil, errors.New("header version tidak didukung")
	}
	var off uint32
	var err error
	take := func(n uint32) []byte {
		if err != nil {
			return nil
		}
		if uint64(off)+uint64(n) > uint64(len(d)) {
			err = errors.New("image terpotong / rusak")
			return nil
		}
		r := append([]byte(nil), d[off:off+n]...)
		return r
	}
	if b.Ver >= 3 {
		b.Page = 4096
		hl := 1580
		if b.Ver == 4 {
			hl = 1584
		}
		b.Hdr = append([]byte(nil), d[:hl]...)
		ks, rs := le32(d[8:]), le32(d[12:])
		off = 4096
		b.Kernel = take(ks)
		off += align(ks, 4096)
		b.Ramdisk = take(rs)
		off += align(rs, 4096)
		if b.Ver == 4 {
			b.Sig = take(le32(d[1580:]))
		}
		return b, err
	}
	b.Page = le32(d[36:])
	if b.Page == 0 || b.Page&(b.Page-1) != 0 {
		return nil, errors.New("page size tidak valid")
	}
	hl := 1632
	if b.Ver == 1 {
		hl = 1648
	} else if b.Ver == 2 {
		hl = 1660
	}
	b.Hdr = append([]byte(nil), d[:hl]...)
	ks, rs, ss := le32(d[8:]), le32(d[16:]), le32(d[24:])
	off = b.Page
	b.Kernel = take(ks)
	off += align(ks, b.Page)
	b.Ramdisk = take(rs)
	off += align(rs, b.Page)
	b.Second = take(ss)
	off += align(ss, b.Page)
	if b.Ver >= 1 {
		ds := le32(d[1632:])
		b.Dtbo = take(ds)
		off += align(ds, b.Page)
	}
	if b.Ver >= 2 {
		b.Dtb = take(le32(d[1648:]))
	}
	return b, err
}

func (b *Boot) Cmdline() string {
	if b.Ver >= 3 {
		return cstr(b.Hdr[44:1580])
	}
	return cstr(b.Hdr[64:576]) + cstr(b.Hdr[608:1632])
}

func (b *Boot) SetCmdline(s string) error {
	clear := func(f []byte) {
		for i := range f {
			f[i] = 0
		}
	}
	if b.Ver >= 3 {
		if len(s) > 1535 {
			return errors.New("cmdline terlalu panjang (max 1535)")
		}
		f := b.Hdr[44:1580]
		clear(f)
		copy(f, s)
		return nil
	}
	if len(s) > 1534 {
		return errors.New("cmdline terlalu panjang (max 1534)")
	}
	clear(b.Hdr[64:576])
	clear(b.Hdr[608:1632])
	if len(s) <= 511 {
		copy(b.Hdr[64:576], s)
	} else {
		copy(b.Hdr[64:576], s[:511])
		copy(b.Hdr[608:1632], s[511:])
	}
	return nil
}

func (b *Boot) Build() []byte {
	h := append([]byte(nil), b.Hdr...)
	var out bytes.Buffer
	padTo := func(p uint32) {
		if r := uint32(out.Len()) % p; r != 0 {
			out.Write(make([]byte, p-r))
		}
	}
	if b.Ver >= 3 {
		put32(h[8:], uint32(len(b.Kernel)))
		put32(h[12:], uint32(len(b.Ramdisk)))
		if b.Ver == 4 {
			put32(h[1580:], uint32(len(b.Sig)))
		}
	} else {
		put32(h[8:], uint32(len(b.Kernel)))
		put32(h[16:], uint32(len(b.Ramdisk)))
		put32(h[24:], uint32(len(b.Second)))
		if b.Ver >= 1 {
			put32(h[1632:], uint32(len(b.Dtbo)))
			off := uint64(b.Page) +
				uint64(align(uint32(len(b.Kernel)), b.Page)) +
				uint64(align(uint32(len(b.Ramdisk)), b.Page)) +
				uint64(align(uint32(len(b.Second)), b.Page))
			binary.LittleEndian.PutUint64(h[1636:], off)
		}
		if b.Ver >= 2 {
			put32(h[1648:], uint32(len(b.Dtb)))
		}
		sh := sha1.New()
		w := func(d []byte) {
			sh.Write(d)
			var t [4]byte
			put32(t[:], uint32(len(d)))
			sh.Write(t[:])
		}
		w(b.Kernel)
		w(b.Ramdisk)
		w(b.Second)
		if b.Ver >= 1 {
			w(b.Dtbo)
		}
		if b.Ver >= 2 {
			w(b.Dtb)
		}
		for i := 576; i < 608; i++ {
			h[i] = 0
		}
		copy(h[576:], sh.Sum(nil))
	}
	out.Write(h)
	padTo(b.Page)
	for _, s := range [][]byte{b.Kernel, b.Ramdisk, b.Second, b.Dtbo, b.Dtb, b.Sig} {
		if len(s) == 0 {
			continue
		}
		out.Write(s)
		padTo(b.Page)
	}
	return out.Bytes()
}

func (b *Boot) Info() {
	fmt.Printf("Header version : %d\nPage size      : %d\n", b.Ver, b.Page)
	fmt.Printf("Kernel         : %s\nRamdisk        : %s\n", human(int64(len(b.Kernel))), human(int64(len(b.Ramdisk))))
	if len(b.Second) > 0 {
		fmt.Printf("Second         : %s\n", human(int64(len(b.Second))))
	}
	if len(b.Dtbo) > 0 {
		fmt.Printf("Recovery DTBO  : %s\n", human(int64(len(b.Dtbo))))
	}
	if len(b.Dtb) > 0 {
		fmt.Printf("DTB            : %s\n", human(int64(len(b.Dtb))))
	}
	if len(b.Sig) > 0 {
		fmt.Printf("Boot signature : %s (akan jadi tidak valid setelah patch)\n", human(int64(len(b.Sig))))
	}
	fmt.Printf("Cmdline        : %s\n", b.Cmdline())
}

func loadBoot(p string) (*Boot, error) {
	d, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return parseBoot(d)
}

func bootInfo() {
	b, err := loadBoot(ask("Path boot.img: "))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	b.Info()
}

func unpackBoot() {
	b, err := loadBoot(ask("Path boot.img: "))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	out := ask("Folder output [unpacked]: ")
	if out == "" {
		out = "unpacked"
	}
	os.MkdirAll(out, 0755)
	save := func(n string, d []byte) {
		if len(d) > 0 {
			if err := os.WriteFile(filepath.Join(out, n), d, 0644); err != nil {
				fmt.Println("Error:", err)
				return
			}
			fmt.Printf("  -> %s (%s)\n", n, human(int64(len(d))))
		}
	}
	save("kernel", b.Kernel)
	save("ramdisk.img", b.Ramdisk)
	save("second", b.Second)
	save("recovery_dtbo", b.Dtbo)
	save("dtb", b.Dtb)
	save("cmdline.txt", []byte(b.Cmdline()))
	b.Info()
}

func repackBoot() {
	p := ask("Path boot.img asli: ")
	b, err := loadBoot(p)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	b.Info()
	repl := func(label string, dst *[]byte) bool {
		v := ask(label + " baru (kosong = tetap): ")
		if v == "" {
			return true
		}
		d, err := os.ReadFile(v)
		if err != nil {
			fmt.Println("Error:", err)
			return false
		}
		*dst = d
		return true
	}
	if !repl("Kernel", &b.Kernel) || !repl("Ramdisk", &b.Ramdisk) {
		return
	}
	if b.Ver == 2 && !repl("DTB", &b.Dtb) {
		return
	}
	if v := ask("Cmdline baru ('+teks' = tambahkan, kosong = tetap): "); v != "" {
		if strings.HasPrefix(v, "+") {
			v = strings.TrimSpace(b.Cmdline() + " " + strings.TrimPrefix(v, "+"))
		}
		if err := b.SetCmdline(v); err != nil {
			fmt.Println("Error:", err)
			return
		}
	}
	def := strings.TrimSuffix(p, filepath.Ext(p)) + "_patched.img"
	out := ask(fmt.Sprintf("Output [%s]: ", def))
	if out == "" {
		out = def
	}
	if err := os.WriteFile(out, b.Build(), 0644); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Selesai ->", out)
	fmt.Println("Catatan: kalau device pakai AVB, flash vbmeta dengan disable verity & verification.")
}

func cleanHex(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	return strings.ReplaceAll(strings.ToLower(s), "0x", "")
}

func hexPatch() {
	p := ask("File (kernel/dll): ")
	d, err := os.ReadFile(p)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fb, err1 := hex.DecodeString(cleanHex(ask("Cari (hex, mis. DE AD BE EF): ")))
	rb, err2 := hex.DecodeString(cleanHex(ask("Ganti (hex, panjang harus sama): ")))
	if err1 != nil || err2 != nil || len(fb) == 0 || len(fb) != len(rb) {
		fmt.Println("Hex tidak valid / panjang tidak sama.")
		return
	}
	n := bytes.Count(d, fb)
	fmt.Printf("Ditemukan %d kecocokan.\n", n)
	if n == 0 {
		return
	}
	def := p + ".patched"
	out := ask(fmt.Sprintf("Output [%s]: ", def))
	if out == "" {
		out = def
	}
	if err := os.WriteFile(out, bytes.ReplaceAll(d, fb, rb), 0644); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Selesai ->", out)
}

func patchMenu() {
	for {
		fmt.Println(`
--- Patch Kernel / boot.img ---
1. Info boot.img
2. Unpack (ekstrak kernel, ramdisk, dll)
3. Repack / patch (ganti kernel, ramdisk, dtb, cmdline)
4. Hex patch (cari & ganti byte)
0. Kembali`)
		switch ask("Pilih: ") {
		case "1":
			bootInfo()
		case "2":
			unpackBoot()
		case "3":
			repackBoot()
		case "4":
			hexPatch()
		case "0":
			return
		}
	}
}

func main() {
	defer ask("\nTekan Enter untuk keluar...")
	fmt.Println("=== VinzFlash - simple Android flash & patch tool ===")
	fmt.Println("Butuh adb/fastboot (platform-tools) di folder yang sama atau di PATH.")
	for {
		fmt.Println(`
1. Lihat isi folder
2. Info file (ukuran, SHA256, tipe)
3. Cek device (adb + fastboot)
4. Masuk fastboot mode
5. Flash file ke partisi
6. Fastboot lainnya (boot sementara, getvar, reboot, unlock)
7. Patch kernel / boot.img
0. Keluar`)
		switch ask("Pilih: ") {
		case "1":
			listDir()
		case "2":
			fileInfo()
		case "3":
			checkDevices()
		case "4":
			enterFastboot()
		case "5":
			flashFile()
		case "6":
			fastbootMore()
		case "7":
			patchMenu()
		case "0", "q":
			return
		default:
			fmt.Println("Pilihan tidak valid.")
		}
	}
}
