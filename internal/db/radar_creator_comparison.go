package db

import (
	"strings"
)

// CreatorCompareDay is one calendar day of real radar observations for one
// creator. A day exists only when the store actually holds an observation for
// that creator on that date — the aggregation never fabricates zero rows to
// fill a window, because a missing day and a real zero day are different facts.
type CreatorCompareDay struct {
	Date         string `json:"date"`
	Likes        int64  `json:"likes"`
	Collects     int64  `json:"collects"`
	Shares       int64  `json:"shares"`
	Comments     int64  `json:"comments"`
	Observations int    `json:"observations"`
}

// CreatorCompareSeries is one creator's real collected activity inside the
// requested window: which works were seen and what their counters did per day.
type CreatorCompareSeries struct {
	Platform string `json:"platform"`
	Author   string `json:"author"`
	// Works counts distinct works observed inside the window; Observations
	// counts the raw observation rows those works contributed.
	Works        int                `json:"works"`
	Observations int                `json:"observations"`
	Days         []CreatorCompareDay `json:"days"`
}

// RadarCreatorComparison aggregates the radar store per creator per day.
//
// fromDate / toDate are inclusive YYYY-MM-DD bounds compared against the
// observed_at date in local time (the same day boundary the UI shows); an
// empty bound means "no limit on that side". platform filters to one platform
// and an empty platform keeps every platform.
//
// Rows with an empty author are skipped: the radar store records unnamed
// entries for search results that never carried an author, and there is
// nothing to compare under an empty label. The window keeps only days that
// really have observations; callers mark sparse series as insufficient rather
// than having this layer interpolate or pad them.
func (db *DB) RadarCreatorComparison(fromDate, toDate, platform string) ([]CreatorCompareSeries, error) {
	clauses := []string{"w.author <> ''"}
	args := []any{}
	if fromDate != "" {
		clauses = append(clauses, "date(o.observed_at,'localtime') >= ?")
		args = append(args, fromDate)
	}
	if toDate != "" {
		clauses = append(clauses, "date(o.observed_at,'localtime') <= ?")
		args = append(args, toDate)
	}
	if platform != "" {
		clauses = append(clauses, "w.platform = ?")
		args = append(args, platform)
	}
	where := "WHERE " + strings.Join(clauses, " AND ")

	rows, err := db.Query(`SELECT w.platform, w.author, date(o.observed_at,'localtime') AS day,
		SUM(o.likes), SUM(o.collects), SUM(o.shares), SUM(o.comments),
		COUNT(o.id)
		FROM radar_observations o
		JOIN radar_works w ON w.id = o.work_pk
		`+where+`
		GROUP BY w.platform, w.author, day
		ORDER BY w.platform, w.author, day`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	series := []CreatorCompareSeries{}
	index := map[string]int{}
	for rows.Next() {
		var (
			platformCode string
			author       string
			day          CreatorCompareDay
		)
		if err := rows.Scan(&platformCode, &author, &day.Date,
			&day.Likes, &day.Collects, &day.Shares, &day.Comments,
			&day.Observations); err != nil {
			return nil, err
		}
		key := platformCode + "\x00" + author
		position, found := index[key]
		if !found {
			series = append(series, CreatorCompareSeries{
				Platform: platformCode,
				Author:   author,
				Days:     []CreatorCompareDay{},
			})
			position = len(series) - 1
			index[key] = position
		}
		series[position].Days = append(series[position].Days, day)
		series[position].Observations += day.Observations
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return series, nil
	}

	// Distinct works per creator across the whole window. Counting per day and
	// adding up would double-count a work observed on two days, so it is its own
	// aggregate over the same window.
	workRows, err := db.Query(`SELECT w.platform, w.author, COUNT(DISTINCT o.work_pk)
		FROM radar_observations o
		JOIN radar_works w ON w.id = o.work_pk
		`+where+`
		GROUP BY w.platform, w.author`, args...)
	if err != nil {
		return nil, err
	}
	defer workRows.Close()
	for workRows.Next() {
		var (
			platformCode string
			author       string
			works        int
		)
		if err := workRows.Scan(&platformCode, &author, &works); err != nil {
			return nil, err
		}
		if position, found := index[platformCode+"\x00"+author]; found {
			series[position].Works = works
		}
	}
	if err := workRows.Err(); err != nil {
		return nil, err
	}
	return series, nil
}
