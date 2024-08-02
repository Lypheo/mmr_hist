A simple tool to extract and plot MMR over time data via the Game Coordinator API.

![example plot](https://raw.githubusercontent.com/Lypheo/mmr_hist/e9a6fb6559806762e15b210ba80d2bb68f64f0a2/time_series.svg)

#### Usage

```sh
mmr_hist.exe <steam username> <steam password>
```
(steam guard probably needs to be deactivated for the login to work)

The tool will attempt to gradually fetch your entire match history in increments of 20 matches.
Once finished, it will dump the retrieved data in a mmr_hist.csv file and draw a graph in a mmr_hist.svg file.
Note that if you’re a degen with too many games on your account (or if you run the tool repeatedly),
you might exceed the API endpoints’s rate limit (something like 500 requests I think),
which will prevent you from loading any match histories (in the client as well) for about a day.

##### How

Dota stores each player’s current MMR at the beginning of every match,
and this data can be retrieved through the [match history API endpoint](https://github.com/paralin/go-dota2/blob/e8f172852608601dcb13ebc8aa442ced27938ad5/protocol/dota_gcmessages_client.proto#L749).
Sadly, this endpoint is only covered in the [go-dota2](https://github.com/paralin/go-dota2/) project,
all other Game Coordinator implementations are too outdated (which is why this is written in go even though I dont know jackshit about the language).
