package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"image/color"
	"log"
	"os"
	"sort" // Add this line
	"strconv"
	"strings"
	"time"

	"git.sr.ht/~sbinet/epok"
	"github.com/paralin/go-dota2"
	devents "github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	"github.com/paralin/go-steam"
	"github.com/sirupsen/logrus"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

func establishDotaHello(d2 *dota2.Dota2, done chan struct{}, limit int) {
	d2.SetPlaying(true)
	time.Sleep(1 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	elapsed := 0
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			d2.SayHello()
			elapsed += 5
			if elapsed > limit {
				fmt.Println("Took too long to connect to Dota 2 GC")
				close(done)
				d2.Close()
				return
			}
		}
	}
}

func writeCSV(mmr_hist []Tuple) {
	file, err := os.Create("mmr_hist.csv")
	if err != nil {
		logrus.Fatal("Cannot create file", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"Date", "Unix time", "MatchID", "MMR"}); err != nil {
		logrus.Fatal("Cannot write header", err)
	}
	for i := len(mmr_hist) - 1; i >= 0; i-- {
		value := mmr_hist[i]
		t := time.Unix(int64(value.Date), 0)
		err := writer.Write([]string{fmt.Sprint(t.Format("2006-01-02 15:04:05")), fmt.Sprint(value.Date),
			fmt.Sprint(value.MatchID), fmt.Sprint(value.MMR)})
		if err != nil {
			logrus.Fatal("Cannot write to file", err)
		}
	}
}

func readCSV() []Tuple {
	file, err := os.Open("mmr_hist.csv")
	if err != nil {
		logrus.Info("Cannot open file: ", err)
		return []Tuple{}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		logrus.Fatal("Cannot read file", err)
	}

	var mmr_hist []Tuple
	for i, record := range records {
		if i == 0 {
			continue
		}
		unix_time, err := strconv.ParseUint(record[1], 10, 32)
		if err != nil {
			logrus.Fatal("Cannot parse unix time", err)
		}
		matchID, err := strconv.ParseUint(record[2], 10, 64)
		if err != nil {
			logrus.Fatal("Cannot parse match ID", err)
		}
		mmr, err := strconv.ParseUint(record[3], 10, 32)
		if err != nil {
			logrus.Fatal("Cannot parse MMR", err)
		}
		mmr_hist = append([]Tuple{{uint32(unix_time), uint32(mmr), matchID}}, mmr_hist...)
	}
	return mmr_hist
}

type Tuple struct {
	Date    uint32
	MMR     uint32
	MatchID uint64
}

func main() {
	logger := logrus.New()
	logger.Out = os.Stdout
	logger.Level = logrus.InfoLevel
	logger.Formatter = &logrus.TextFormatter{
		FullTimestamp: true,
	}

	if len(os.Args) < 3 {
		logger.Error("Usage: mmr_hist <username> <password> [<max number of pages to fetch> <verbose?>]")
		return
	}

	client := steam.NewClient()
	steam.InitializeSteamDirectory()
	client.Connect()
	d2 := dota2.New(client, logger)
	defer client.Disconnect()
	defer d2.Close()
	hello_done := make(chan struct{})
	done := false

	loginDetails := steam.LogOnDetails{
		Username:               os.Args[1],
		Password:               os.Args[2],
		ShouldRememberPassword: true, // doesnt seem to do anything?
	}

	// plotMMR(d2, client, logger)
	// os.Exit(0)

	for event := range client.Events() {
		switch e := event.(type) {
		case *steam.LogOnFailedEvent:
			logger.Info("Loging on to Steam failed: ", e.Result)
			fmt.Println("Enter your steam guard code: ")
			var authcode string
			fmt.Scanln(&authcode)
			var method string
			fmt.Println("Steam guard method (1 for email, 2 for mobile): ")
			fmt.Scanln(&method)
			if strings.Contains(method, "1") {
				loginDetails.AuthCode = authcode
			} else {
				loginDetails.TwoFactorCode = authcode
			}
			client.Connect()
		case *steam.ConnectedEvent:
			logger.Info("Connected to Steam")
			client.Auth.LogOn(&loginDetails)
		case *steam.LoggedOnEvent:
			logger.Info("Logged on to Steam")
			go establishDotaHello(d2, hello_done, 60)
		case *devents.ClientWelcomed:
			logger.Info("Welcomed to Dota 2")
			hello_done <- struct{}{}
			// go plotMMR(d2, client, logger)
			plotMMR(d2, client, logger)
			done = true
			d2.Close()
			client.Disconnect()
			os.Exit(0)
		case *steam.DisconnectedEvent:
			logger.Debug("Disconnected from Steam")
			if done {
				return
			}
		case steam.FatalErrorEvent:
			logger.Errorf("Fatal error: %v", e)
			return
			// case steam.LoginKeyEvent:
			// 	logger.Warn(e.LoginKey)
			// 	logger.Warn("Received login key")
			// case steam.MachineAuthUpdateEvent:
			// 	logger.Warn(e.Hash)
			// 	logger.Warn("Received machine auth update")
			// default:
			// logger.Warn(e)
		}
	}
}

func plotMMR(d2 *dota2.Dota2, client *steam.Client, logger *logrus.Logger) {
	var mmr_hist []Tuple = readCSV()

	details := protocol.CMsgDOTAGetPlayerMatchHistory{}

	details.AccountId = new(uint32)
	steam3string := client.SteamId().ToSteam3()
	steam3Parts := strings.Split(steam3string, ":")
	steam3 := steam3Parts[len(steam3Parts)-1]
	steam3 = steam3[:len(steam3)-1] // remove ']'
	steam3int, err := strconv.ParseUint(steam3, 10, 32)
	if err != nil {
		log.Fatal("Failed to convert steam3 to uint32", err)
	}
	*details.AccountId = uint32(steam3int)

	details.MatchesRequested = new(uint32)
	*details.MatchesRequested = 20

	pages := 999999
	if len(os.Args) >= 4 {
		pages, err = strconv.Atoi(os.Args[3])
		if err != nil {
			log.Fatal("Failed to convert pages to integer", err)
		}
	}

	latest_saved_mid := uint64(0)
	if len(mmr_hist) > 0 {
		latest_saved_mid = mmr_hist[len(mmr_hist)-1].MatchID
	}
outer:
	for i := 0; i < pages; i++ {
		hist, err := d2.GetPlayerMatchHistory(context.TODO(), &details)
		if err != nil || len(hist.Matches) == 0 {
			logger.Println(err)
			break
		}
		if details.StartAtMatchId == nil {
			details.StartAtMatchId = new(uint64)
		}
		for _, match := range hist.Matches {
			if len(os.Args) >= 5 && match.RankChange != nil {
				logger.Println(*match.RankChange)
			}
			if match.StartTime != nil && match.PreviousRank != nil && match.MatchId != nil && *match.PreviousRank != 0 {
				if *match.MatchId == latest_saved_mid {
					break outer
				}
				mmr_hist = append(mmr_hist, Tuple{*match.StartTime, *match.PreviousRank, *match.MatchId})
				*details.StartAtMatchId = *match.MatchId
			}
		}
		fmt.Printf("\rProgress: %d", len(mmr_hist))
		time.Sleep(500 * time.Millisecond)
	}

	// Sort mmr_hist by date
	sort.Slice(mmr_hist, func(i, j int) bool {
		return mmr_hist[i].Date < mmr_hist[j].Date
	})

	if len(mmr_hist) == 0 {
		logrus.Fatal("Failed fetching matches")
		return
	}
	writeCSV(mmr_hist)

	pts := make(plotter.XYs, 0)
	for _, tuple := range mmr_hist {
		if tuple.MMR == 0 || tuple.Date == 0 {
			continue
		}
		pt := plotter.XY{X: float64(tuple.Date) * 1_000_000_000, Y: float64(tuple.MMR)}
		pts = append(pts, pt)
	}

	p := hplot.New()

	p.Title.Text = "MMR Over Time"
	p.X.Label.Text = "Time"
	p.Y.Label.Text = "MMR"
	p.X.AutoRescale = true
	cnv := epok.UTCUnixTimeConverter{}

	p.Y.Tick.Marker = hplot.Ticks{N: 10}

	months := (mmr_hist[len(mmr_hist)-1].Date - mmr_hist[0].Date) / 2_592_000
	freq := max(int(months/12), 4)
	p.X.Tick.Marker = epok.Ticks{
		Ruler: epok.Rules{
			Major: epok.Rule{
				Freq:  epok.Monthly,
				Range: epok.RangeFrom(1, 13, freq),
			},
		},
		Format:    "2006-01",
		Converter: cnv,
	}

	line, pnts, err := hplot.NewLinePoints(pts)
	if err != nil {
		log.Fatalf("could not create plotter: %+v", err)
	}

	line.Color = color.RGBA{B: 255, A: 255}
	pnts.Shape = draw.CircleGlyph{}
	pnts.Color = color.RGBA{R: 255, A: 255}
	pnts.Radius = vg.Points(1)

	p.Add(line, pnts, hplot.NewGrid())

	var maxMMR, minMMR uint32
	for _, tuple := range mmr_hist {
		if tuple.MMR > maxMMR {
			maxMMR = tuple.MMR
		}
		if tuple.MMR < minMMR || minMMR == 0 {
			minMMR = tuple.MMR
		}
	}
	fmt.Printf("Max MMR: %d\n", maxMMR)
	fmt.Printf("Min MMR: %d\n", minMMR)

	// width := max(1+vg.Length(months)*vg.Centimeter*1.5, 40*vg.Centimeter)
	// height := max(2+vg.Length(maxMMR-minMMR)*vg.Centimeter/100, 4*vg.Centimeter)
	width := 45 * vg.Centimeter
	height := 20 * vg.Centimeter
	if err := p.Save(width, height, "mmr_hist.svg"); err != nil {
		logger.Panic(err)
	}
}
