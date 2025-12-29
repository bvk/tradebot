// Copyright (c) 2025 BVK Chaitanya

package waller

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type WallerSummaryItem struct {
	UID  string
	Name string

	Profit    decimal.Decimal
	ReturnPct decimal.Decimal
	APR       decimal.Decimal

	NumDays decimal.Decimal

	*gobs.JobData
	*gobs.Summary
}

type Summary struct {
	cmdutil.DBFlags

	beginTime, endTime string

	recal bool

	states string

	format string

	templateForm string

	tableColumns      string
	tableTotalColumns string

	skipUninteresting bool
}

func (c *Summary) Purpose() string {
	return "Prints summary of all or selected wallers"
}

func (c *Summary) Description() string {
	return `

This "summary" subcommand prints one or more or all waller instances summary in
multiple formats. Users can specify the waller instances as one or more command
line arguments. All waller instances are selected when no arguments are
specified. Users can use -f flag with one of json|table|template arguments to
select the output format.

When json format is selected, waller summary will be printed as an array of
json messages. When table format is selected, users can use -table-* flags to
pick which columns to print. When template format is selected, users are
expected to specify --template-form argument as per the rules of the Go
standard library package text/template. Some examples are given below.

EXAMPLES

    # Print all waller instances in json format from the beginning of the bot

    $ tradebot waller summary -f json

    # Print all waller instances in json format for a specific time duration

    $ tradebot waller summary -f json -begin-time 2024-01-01 -end-time 2025-01-01

    # Print selected waller instances in table format

    $ tradebot waller summary -f table bch-200-300-2x

    # Print selected waller instance profit

    $ tradebot waller summary -f template --template-form '{{.Profit}}' bch-200-300-2x

    # Print profit for all waller instances along with their job names

    $ tradebot waller summary -f template --template-form '{{.Profit}} -- {{.Name}}'

`
}

func (c *Summary) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("summary", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.beginTime, "begin-time", "", "Begin time for summary time period")
	fset.StringVar(&c.endTime, "end-time", "", "End time for summary time period")
	fset.StringVar(&c.states, "states", "", "When non-empty filters out jobs by their state")
	fset.StringVar(&c.format, "f", "table", "Printed output format (table|json|template)")
	fset.StringVar(&c.templateForm, "template-form", "", "Golang text/template for printing summary")
	fset.StringVar(&c.tableColumns, "table-columns", "Name,State,ProductID,BeginAt,EndAt,Budget,Profit,ReturnPct,NumDays,APR,,NumBuys,NumSells,BoughtFees,BoughtSize,BoughtValue,SoldFees,SoldSize,SoldValue,UnsoldFees,UnsoldSize,UnsoldValue,OversoldFees,OversoldSize,OversoldValue", "Columns to print in the table form")
	fset.StringVar(&c.tableTotalColumns, "table-total-columns", "Profit,BoughtFees,BoughtValue,SoldFees,SoldValue,UnsoldFees,UnsoldValue,OversoldFees,OversoldValue", "Columns to print in the table totals line")
	fset.BoolVar(&c.recal, "recalculate", false, "When true, summary information is recalculated")
	fset.BoolVar(&c.skipUninteresting, "skip-uninteresting", true, "When false, summary info is printed for all wallers")
	return "summary", fset, cli.CmdFunc(c.run)
}

func (c *Summary) run(ctx context.Context, args []string) error {
	// Prepare a time-period if it was given.
	now := time.Now()
	var period *timerange.Range
	parseTime := func(s string) (time.Time, error) {
		if d, err := time.ParseDuration(s); err == nil {
			return now.Add(d), nil
		}
		if v, err := time.Parse("2006-01-02", s); err == nil {
			return v, nil
		}
		return time.Parse(time.RFC3339, s)
	}
	if len(c.beginTime) != 0 || len(c.endTime) != 0 {
		period = &timerange.Range{End: now}
	}
	if len(c.beginTime) > 0 {
		v, err := parseTime(c.beginTime)
		if err != nil {
			return err
		}
		period.Begin = v
	}
	if len(c.endTime) > 0 {
		v, err := parseTime(c.endTime)
		if err != nil {
			return err
		}
		period.End = v
	}

	var states []gobs.State
	if len(c.states) != 0 {
		valid := []gobs.State{gobs.PAUSED, gobs.RUNNING, gobs.COMPLETED, gobs.CANCELED, gobs.FAILED}
		for _, s := range strings.Split(c.states, ",") {
			state := gobs.State(strings.ToUpper(s))
			if !slices.Contains(valid, state) {
				return fmt.Errorf("invalid job state %q", s)
			}
			states = append(states, state)
		}
	}

	// Open the database.
	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	runner := job.NewRunner(db)

	uid2nameMap := make(map[string]string)
	uid2jdMap := make(map[string]*gobs.JobData)
	uid2sumMap := make(map[string]*gobs.Summary)
	collect := func(ctx context.Context, r kv.Reader, key string, wstate *gobs.WallerState) error {
		uid := strings.TrimPrefix(key, waller.DefaultKeyspace)
		fs := strings.Split(uid, "/")
		if len(fs) == 0 {
			return fmt.Errorf("unexpected key format %q under waller keyspace", key)
		}
		jobUUID := fs[0]
		if _, err := uuid.Parse(fs[0]); err != nil {
			return fmt.Errorf("could not parse waller uuid from the key %q: %v", key, err)
		}
		jd, err := runner.Get(ctx, r, jobUUID)
		if err != nil {
			return fmt.Errorf("could not determine job state for the waller at key %q: %v", key, err)
		}
		if len(states) != 0 && !slices.Contains(states, jd.State) {
			return nil // Skip the waller using -state flag value as the filter by job state.
		}

		jobName, jobUUID2, jobType, err := namer.Resolve(ctx, r, jobUUID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			// User-defined name may not exist for job, so use sensible defaults.
			jobName, jobUUID2, jobType = uid, jobUUID, "waller"
		}
		if jobUUID != jobUUID2 {
			return fmt.Errorf("unexpected: waller uid field doesn't match the job-uuid (%q != %q)", jobUUID, jobUUID2)
		}
		if !strings.EqualFold(jobType, "waller") {
			return fmt.Errorf("unexpected non-waller job type %q for key %q", jobType, key)
		}
		if len(args) != 0 && !slices.Contains(args, jobName) && !slices.Contains(args, uid) {
			return nil // Skip the waller using cli arguments as the filter by job name/uid.
		}

		// Load the summary.
		sum, err := waller.Summary(ctx, r, uid, period, c.recal)
		if err != nil {
			return err
		}

		if period != nil {
			if sum.BeginAt.IsZero() {
				sum.BeginAt = period.Begin
			}
			if sum.EndAt.IsZero() {
				sum.EndAt = period.End
			}
			if jd.State.IsRunning() {
				sum.EndAt = period.End
			}
		}
		if jd.State.IsRunning() {
			if sum.EndAt.IsZero() {
				sum.EndAt = now
			}
		}

		// Skip zero summaries for uninteresting wallers.
		if sum.IsZero() && c.skipUninteresting {
			if len(states) == 0 && !jd.State.IsRunning() {
				return nil
			}
			if len(states) != 0 && !slices.Contains(states, jd.State) {
				return nil
			}
		}

		uid2jdMap[uid] = jd
		uid2sumMap[uid] = sum
		uid2nameMap[uid] = jobName
		return nil
	}
	beg, end := kvutil.PathRange(waller.DefaultKeyspace)
	if err := kvutil.AscendDB[gobs.WallerState](ctx, db, beg, end, collect); err != nil {
		return err
	}

	// Pick a job order: JobState, Product, NumDays, JobName order.
	uids := slices.Collect(maps.Keys(uid2jdMap))
	order := []gobs.State{gobs.RUNNING, gobs.PAUSED, gobs.COMPLETED, gobs.FAILED, gobs.CANCELED}
	sort.SliceStable(uids, func(i, j int) bool {
		s1, s2 := uid2jdMap[uids[i]].State, uid2jdMap[uids[j]].State
		if x, y := slices.Index(order, s1), slices.Index(order, s2); x != y {
			return x < y
		}
		if p1, p2 := uid2sumMap[uids[i]].ProductID, uid2sumMap[uids[j]].ProductID; p1 != p2 {
			return p1 < p2
		}
		if t1, t2 := uid2sumMap[uids[i]].BeginAt, uid2sumMap[uids[j]].BeginAt; !t1.Equal(t2) {
			return t1.Before(t2)
		}
		n1, _ := uid2nameMap[uids[i]]
		n2, _ := uid2nameMap[uids[j]]
		return n1 < n2
	})

	var items []*WallerSummaryItem
	for _, uid := range uids {
		item := &WallerSummaryItem{
			UID:     uid,
			Name:    uid2nameMap[uid],
			Summary: uid2sumMap[uid],
			JobData: uid2jdMap[uid],
		}
		item.NumDays = item.Summary.NumDays()
		item.Profit = item.Summary.Profit()
		item.ReturnPct = item.Summary.ReturnPct()
		item.APR = item.Summary.AnnualPct()
		items = append(items, item)
	}

	switch strings.ToLower(c.format) {
	case "json":
		return c.printJSON(os.Stdout, items)
	case "template":
		return c.printTemplate(os.Stdout, items)
	case "table":
		return c.printTable(os.Stdout, items)
	default:
		return fmt.Errorf("unknown/invalid print format %q", c.format)
	}
}

func (c *Summary) printJSON(w io.Writer, items []*WallerSummaryItem) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func (c *Summary) printTemplate(w io.Writer, items []*WallerSummaryItem) error {
	format := "{{.}}"
	if len(c.templateForm) != 0 {
		format = c.templateForm
	}

	tmpl, err := template.New("print").Parse(format + "\n")
	if err != nil {
		return fmt.Errorf("could not parse print-template: %w", err)
	}

	for _, item := range items {
		if err := tmpl.Execute(w, item); err != nil {
			return fmt.Errorf("could not execute the format template: %v", err)
		}
	}
	return nil
}

func (c *Summary) printTable(w io.Writer, items []*WallerSummaryItem) error {
	cols := strings.Split(c.tableColumns, ",")

	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', tabwriter.AlignRight)

	// Create the format strings.
	var titleBuf strings.Builder
	var formatBuf strings.Builder

	fields := reflect.VisibleFields(reflect.TypeFor[WallerSummaryItem]())
	slices.SortFunc(fields, func(f1, f2 reflect.StructField) int {
		return cmp.Compare(slices.Index(cols, f1.Name), slices.Index(cols, f2.Name))
	})
	col2fieldMap := make(map[int][]int)
	for i, field := range fields {
		if field.Anonymous || !field.IsExported() || !slices.Contains(cols, field.Name) {
			continue
		}

		col2fieldMap[i] = field.Index
		titleBuf.Write([]byte(field.Name))
		titleBuf.WriteRune('\t')

		formatBuf.Write([]byte("%v"))
		formatBuf.WriteRune('\t')
	}
	titleBuf.WriteRune('\n')
	formatBuf.WriteRune('\n')

	fmt.Fprintf(tw, titleBuf.String())
	for _, item := range items {
		var values []any
		for i := range len(fields) {
			index, ok := col2fieldMap[i]
			if !ok {
				continue
			}
			v := reflect.ValueOf(item).Elem().FieldByIndex(index)
			switch x := v.Interface().(type) {
			case decimal.Decimal:
				values = append(values, x.StringFixed(3))
			case time.Time:
				values = append(values, x.Format("2006-01-02"))
			default:
				values = append(values, v)
			}
		}
		fmt.Fprintf(tw, formatBuf.String(), values...)
	}

	// Compute totals for specific columns.
	totalColumns := strings.Split(c.tableTotalColumns, ",")
	if len(items) > 1 && len(totalColumns) != 0 {
		var totals []any
		for i, field := range fields {
			index, ok := col2fieldMap[i]
			if !ok {
				continue
			}
			if !slices.Contains(totalColumns, field.Name) {
				totals = append(totals, "")
				continue
			}
			var total decimal.Decimal
			for _, item := range items {
				v := reflect.ValueOf(item).Elem().FieldByIndex(index)
				if x, ok := v.Interface().(decimal.Decimal); ok {
					total = total.Add(x)
				}
			}
			totals = append(totals, total.StringFixed(3))
		}
		fmt.Fprintf(tw, formatBuf.String(), totals...)
	}

	tw.Flush()
	return nil
}
