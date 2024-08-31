package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"image/color"
	"log"
	"os"
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
		log.Fatal("Cannot create file", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"Date", "Unix time", "MMR"}); err != nil {
		log.Fatal("Cannot write header", err)
	}

	for _, value := range mmr_hist {
		t := time.Unix(int64(value.Date), 0)
		err := writer.Write([]string{fmt.Sprint(t.Format("2006-01-02 15:04:05")), fmt.Sprint(value.Date), fmt.Sprint(value.MMR)})
		if err != nil {
			log.Fatal("Cannot write to file", err)
		}
	}
}

type Tuple struct {
	Date uint32
	MMR  uint32
}

func main() {
	logger := logrus.New()
	logger.Out = os.Stdout
	logger.Level = logrus.InfoLevel
	logger.Formatter = &logrus.TextFormatter{
		FullTimestamp: true,
	}

	if len(os.Args) < 3 {
		logger.Error("Usage: mmr_hist <username> <password>")
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
	var mmr_hist []Tuple
	var last_mid uint64 = 0

	for {
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
		if last_mid != 0 {
			details.StartAtMatchId = new(uint64)
			*details.StartAtMatchId = last_mid
		}
		hist, err := d2.GetPlayerMatchHistory(context.TODO(), &details)
		if err != nil || len(hist.Matches) == 0 {
			logrus.Println(err)
			break
		}
		for _, match := range hist.Matches {
			last_mid = *match.MatchId
			if len(os.Args) >= 4 {
				logger.Println(*match.RankChange)
			}
			if match.StartTime != nil && match.PreviousRank != nil && *match.PreviousRank != 0 {
				mmr_hist = append([]Tuple{{*match.StartTime, *match.PreviousRank}}, mmr_hist...)
			}
		}
		fmt.Printf("\rProgress: %d", len(mmr_hist))
		time.Sleep(500 * time.Millisecond)
		if len(os.Args) >= 4 {
			return
		}
	}
	if last_mid == 0 {
		logrus.Println("Failed fetching matches")
		return
	}
	writeCSV(mmr_hist)

	months := (mmr_hist[len(mmr_hist)-1].Date - mmr_hist[0].Date) / 2_592_000

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
	p.X.Tick.Marker = epok.Ticks{
		Ruler: epok.Rules{
			Major: epok.Rule{
				Freq:  epok.Monthly,
				Range: epok.RangeFrom(1, 13, 2),
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

	if err := p.Save(vg.Length(months)/3*vg.Inch, vg.Length(months)*vg.Inch/16, "mmr_hist.svg"); err != nil {
		logger.Panic(err)
	}
}
