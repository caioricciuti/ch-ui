---
slug: why-i-couldnt-lose-my-data
title: Why I couldn't Lose My Data!
authors: [caioricciuti]
tags: [CH-UI, Click House, Data Analysis]
---

# Why I couldn't Lose My Data!

Ever wondered how often Brooklynites make a break for the airport? Well, we've crunched the numbers, and the results are in!

## The Brooklyn Escape Statistics

From July 1, 2015 to September 30, 2015, we tracked a whopping **52,075** taxi rides originating from the hipster havens and brownstone-lined streets of Brooklyn. But here's the kicker: **989** of these rides were headed straight for the airport!

:::tip Fun Fact
A full **1.9%** of Brooklyn taxi riders were airport-bound. That's nearly 1 in 50 rides!
:::

## The Great Escape: Theories and Speculations

Are they fleeing the artisanal coffee shops? Escaping the never-ending brunch lines? Or just jetting off on another Instagram-worthy adventure?

We may never know the true reason behind this mass exodus, but one thing's for sure: Brooklyn might be trendy, but apparently, the trendiest destination of all is... anywhere else!

## The Brooklyn Airport Dash: A Modern Urban Legend

So the next time you're stuck in Brooklyn traffic, remember: there's a 1.9% chance the car next to you is making a desperate dash for JFK. Maybe they forgot to pack their artisanal beard oil, or perhaps they're just late for their shift as a professional avocado toast influencer in LA.

:::note The Real Miracle of Brooklyn
For many Brooklynites, the real 'Miracle of Brooklyn' is successfully hailing a cab to the airport!
:::

## What's behind all this madness?

> "How am I going to cary on with my lide without answers for all my questions about taxi rides in NYC!? I CAN'T LOSE MY DATA!!! I NEED to know how many rides it happened from brooking to the airport!"

I couldn't bear not knowing...

So, here's the raw data for all you number-crunching enthusiasts:

<details>
<summary>Query & Results</summary>

```sql
SELECT
  MIN(pickup_datetime) AS earliest_date,
  MAX(pickup_datetime) AS latest_date,
  COUNT(*) AS total_brooklyn_rides,
  SUM(
    CASE
      WHEN dropoff_ntaname = 'Airport' THEN 1
      ELSE 0
    END
  ) AS brooklyn_to_airport_rides,
  ROUND(
    SUM(
      CASE
        WHEN dropoff_ntaname = 'Airport' THEN 1
        ELSE 0
      END
    ) * 100.0 / COUNT(*),
    2
  ) AS brooklyn_to_airport_percentage
FROM
  trips
WHERE
  pickup_ntaname IN (
    'Park Slope-Gowanus',
    'DUMBO-Vinegar Hill-Downtown Brooklyn-Boerum Hill',
    'Williamsburg',
    'Bedford',
    'North Side-South Side',
    'Brooklyn Heights-Cobble Hill',
    'Prospect Heights',
    'Clinton Hill',
    'Carroll Gardens-Columbia Street-Red Hook',
    'Bushwick South',
    'Fort Greene',
    'Crown Heights North',
    'Sunset Park West',
    'Bushwick North',
    'Greenpoint',
    'East Williamsburg',
    'Ocean Hill',
    'Stuyvesant Heights',
    'Kensington-Ocean Parkway',
    'Flatlands',
    'Bay Ridge',
    'Erasmus',
    'Crown Heights South',
    'Windsor Terrace',
    'Flatbush',
    'Dyker Heights',
    'Sunset Park East',
    'Canarsie',
    'East New York',
    'East Flatbush-Farragut',
    'Borough Park',
    'Prospect Lefferts Gardens-Wingate',
    'Seagate-Coney Island',
    'Starrett City',
    'Homecrest',
    'Midwood',
    'Cypress Hills-City Line',
    'Bath Beach',
    'Bensonhurst West',
    'Rugby-Remsen Village',
    'Ocean Parkway South',
    'East New York (Pennsylvania Ave)',
    'Madison',
    'Gravesend',
    'Bensonhurst East',
    'Sheepshead Bay-Gerritsen Beach-Manhattan Beach',
    'Georgetown-Marine Park-Bergen Beach-Mill Basin',
    'Brownsville',
    'Brighton Beach'
  )
```

```json
[
  {
    "earliest_date": "2015-07-01 00:05:52",
    "latest_date": "2015-09-30 23:51:59",
    "total_brooklyn_rides": "52075",
    "brooklyn_to_airport_rides": "989",
    "brooklyn_to_airport_percentage": 1.9
  }
]
```

</details>

:::note
**TRY IT:** Run the query yourself in your ClickHouse database to see the results!

Want to do it your self? Follow the steps [From ClickHouse official website](https://clickhouse.com/docs/en/getting-started/example-datasets/nyc-taxi) guide.

:::
