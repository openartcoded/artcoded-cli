package main

// fl flattens flag docs: strings and []string
func fl(parts ...any) []string {
	var out []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		}
	}
	return out
}

func idArg(what string) string {
	return "<id> | the " + what + " (or --id ID); chosen in a list when missing"
}

func idsArg(what string) string {
	return "<id...> | one or more " + what + " (or --id ID, repeatable); several chosen in a list when missing (1,3,5-7 or all)"
}

var (
	idFlag   = "--id ID | same as the <id> argument"
	pageOpts = []string{"--page N | page number, from 1 (default 1)", "--size N | items per page"}
	outOpts  = []string{
		"--out PATH | file to write, or a directory (ending with /) to save into; default: download_dir of the config, else the current directory",
		"--open | open the file once saved (pdf viewer, browser...)",
		"--stdout | write the file to the standard output instead (pipes)",
	}
	dateHelp = "YYYY-MM-DD, DD/MM/YYYY, today, yesterday, +3d, -2w, +1m"
)

var docs = map[string]Doc{
	// ---------------------------------------------------------------- home
	"home dashboard": {
		Desc: `The figures checked every day (also: artcoded dashboard):
  - key figures: to invoice this month, revenue of the year against the previous one at the same date,
    days invoiced and revenue per day, hours this month, unprocessed expenses, active dossier, notifications;
  - revenue per month of the last 12 months (column chart) and the cumulated revenue of this year against
    the previous one (line chart);
  - the open timesheets, the next tasks, the todos and the unseen notifications.
What the role cannot see is left out. The demo mode of the backoffice (company settings) masks it like --demo.`,
		Examples: []string{"artcoded dashboard", "# while sharing the screen\nartcoded dashboard --demo", "artcoded dashboard --online", "# as json, e.g. for a status bar\nartcoded home dashboard --json"},
	},
	"home todos": {
		Desc:     "The todos of the home page. Done ones are hidden unless --all.",
		Flags:    fl("--all | also the done todos"),
		Examples: []string{"artcoded home todos", "artcoded home todos --all"},
	},
	"home todo-add": {
		Desc:     "Adds a todo. The words of the arguments are the title.",
		Flags:    fl("<title> | the todo (several words are fine, or --title)", "--title TEXT | the todo"),
		Examples: []string{"artcoded home todo-add call the accountant"},
	},
	"home todo-done": {
		Desc:     "Marks todos done, or not done again (it toggles).",
		Flags:    fl(idsArg("todos"), idFlag),
		Examples: []string{"# choose in the list\nartcoded home todo-done", "artcoded home todo-done 6650f1c2e1"},
	},
	"home todo-delete": {
		Desc:     "Deletes todos, after a confirmation.",
		Flags:    fl(idsArg("todos"), idFlag),
		Examples: []string{"artcoded home todo-delete", "artcoded home todo-delete 6650f1c2e1 --yes"},
	},
	"home postits": {
		Desc:     "The post-its (notes) of the home page.",
		Examples: []string{"artcoded home postits"},
	},
	"home postit-add": {
		Desc:     "Adds a post-it. Without text, the editor opens (multi-line notes).",
		Flags:    fl("<note> | the text (several words), or --note", "--note TEXT | the text", "--note-file FILE | the text from a file (- : standard input)"),
		Examples: []string{"artcoded home postit-add renew the domain in march", "echo hello | artcoded home postit-add --note-file -"},
	},
	"home postit-delete": {
		Desc:     "Deletes post-its, after a confirmation.",
		Flags:    fl(idsArg("post-its"), idFlag),
		Examples: []string{"artcoded home postit-delete"},
	},
	"home memos": {
		Desc:     "The memo dates: things to count the days since (last holiday, last medical check...).",
		Examples: []string{"artcoded home memos"},
	},
	"home memo-add": {
		Desc:     "Adds a memo date.",
		Flags:    fl("<label> | what it is (or --label)", "--label TEXT | what it is", "--date DATE | since when (default today; "+dateHelp+")"),
		Examples: []string{"artcoded home memo-add --label \"last dentist\" --date 2026-03-14"},
	},
	"home memo-delete": {
		Desc:     "Deletes a memo date.",
		Flags:    fl(idArg("memo date"), idFlag),
		Examples: []string{"artcoded home memo-delete"},
	},
	"home notifications": {
		Desc:     "The latest notifications (new expense received, invoice generated, task results...). ● marks the unseen ones.",
		Flags:    fl("--unseen | only the unseen ones"),
		Examples: []string{"artcoded home notifications --unseen"},
	},
	"home notifications-seen": {
		Desc:     "Marks notifications as seen.",
		Flags:    fl(idsArg("notifications"), idFlag, "--all | every notification", "--seen BOOL | false to mark them unseen again (default true)"),
		Examples: []string{"artcoded home notifications-seen --all", "artcoded home notifications-seen 66a1 --seen false"},
	},
	"home notifications-delete": {
		Desc:     "Deletes notifications. --all deletes the seen ones.",
		Flags:    fl(idsArg("notifications"), idFlag, "--all | every seen notification"),
		Examples: []string{"artcoded home notifications-delete --all --yes"},
	},

	// ---------------------------------------------------------------- stats
	"stats years": {
		Desc: `The "Per year" table of the home page: revenue excluding VAT (with a bar), days invoiced, revenue per
day invoiced, and the relative revenue: revenue / calendar days worked from the timesheets (a day counts once,
however many hours are invoiced that day; only the years with a timesheet every month, and this one).
The relative column needs the timesheets (admin).`,
		Examples: []string{"artcoded stats years", "artcoded stats years --json"},
	},
	"stats months": {
		Desc:     "Revenue per month (period of the invoices) of a year: a column chart, then the table with the days invoiced and the revenue per day. This year by default, or the previous one when this one has no invoice yet.",
		Flags:    fl("--year YEAR | another year", "--all | every month since the first invoice"),
		Examples: []string{"artcoded stats months", "artcoded stats months --year 2025", "artcoded stats months --all"},
	},
	"stats clients": {
		Desc:     "Revenue per client, the biggest first, with its share. All time, or one year.",
		Flags:    fl("--year YEAR | only the invoices of this year"),
		Examples: []string{"artcoded stats clients", "artcoded stats clients --year 2026"},
	},
	"stats revenue": {
		Desc:     "Cumulated revenue (excl. VAT, by date of invoice) month by month, one line per year: a line chart (this year in cyan, the previous one in yellow) and the table.",
		Flags:    fl("--years N | how many years (default the last 4)"),
		Examples: []string{"artcoded stats revenue", "artcoded stats revenue --years 2 --demo"},
	},
	"stats hours": {
		Desc:     "Hours worked per year (timesheets), stacked per client: the 5 most worked apart, the others in Other. The chart, then the table.",
		Examples: []string{"artcoded stats hours"},
	},
	"stats hours-progress": {
		Desc:     "Cumulated hours month by month: a line chart (the last 5 years) and the table (every year; this year stops at this month), and this year so far against the same months of the previous one.",
		Flags:    fl("--client NAME | only this client (name contains)"),
		Examples: []string{"artcoded stats hours-progress", "artcoded stats hours-progress --client acme"},
	},
	"stats hours-clients": {
		Desc:     "Hours per client and year, the most worked first, with the progress of this year against the same months of the previous one.",
		Examples: []string{"artcoded stats hours-clients"},
	},
	"stats dossiers": {
		Desc:     "Earnings and expenses per closed dossier, the oldest first.",
		Examples: []string{"artcoded stats dossiers"},
	},
	"stats expenses": {
		Desc:     "Expenses per tag excluding VAT (the VAT aside), the biggest first; the tag OTHER is left out like on the home page.",
		Examples: []string{"artcoded stats expenses"},
	},

	// ---------------------------------------------------------------- tasks
	"tasks list": {
		Desc:     "Every task, the next one first: reminders (a date) and scheduled actions (a cron expression).",
		Examples: []string{"artcoded tasks list", "artcoded tasks list --json | jq '.[].title'"},
	},
	"tasks next": {
		Desc:     "The next ten tasks to run.",
		Examples: []string{"artcoded tasks next"},
	},
	"tasks show": {
		Desc:     "A task with its schedule, its action and the parameters of the action.",
		Flags:    fl(idArg("task"), idFlag),
		Examples: []string{"artcoded tasks show"},
	},
	"tasks add": {
		Desc: `Adds a task. Two kinds:
  - a reminder: a title and a date (notified by mail / sms / in the app);
  - an action: one of 'tasks actions' (backups, reports...), its parameters and a cron schedule.
Without options everything is asked. The cron expression has 6 fields: second minute hour day month weekday.`,
		Flags: fl("<title> | the title (or --title)", "--title TEXT | the title", "--description TEXT | what to do",
			"--kind reminder|action | asked when neither --action nor a schedule is given",
			"--action KEY | the action to run (see tasks actions); without it: a reminder",
			"--param KEY=VALUE | a parameter of the action (repeatable); the missing ones are asked",
			"--custom-name TEXT | a name for the action in the results",
			"--when date|cron | how it is scheduled (asked)", "--cron EXPR | run on this schedule, e.g. \"0 0 8 * * MON-FRI\"",
			"--date \"DATE HH:MM\" | run once at this moment ("+dateHelp+")",
			"--mail | send a mail (--no-mail: no)", "--sms | send a sms", "--in-app | in-app notification (default yes)",
			"--persist | keep the results of the action (default yes)"),
		Examples: []string{
			"# a reminder, asked step by step\nartcoded tasks add",
			"artcoded tasks add \"pay the VAT\" --date \"2026-12-20 09:00\" --mail --sms",
			"# an action every monday at 7\nartcoded tasks add \"weekly dump\" --action DUMP --cron \"0 0 7 * * MON\" --param snapshot=true --yes",
		},
	},
	"tasks edit": {
		Desc:     "Edits a task as json in the editor: title, schedule, parameters... Unknown fields are kept.",
		Flags:    fl(idArg("task"), idFlag),
		Examples: []string{"artcoded tasks edit", "EDITOR=nano artcoded tasks edit 66b2"},
	},
	"tasks enable": {
		Desc:     "Enables a disabled task.",
		Flags:    fl(idArg("task"), idFlag),
		Examples: []string{"artcoded tasks enable 66b2"},
	},
	"tasks disable": {
		Desc:     "Disables a task: it stays, but does not run.",
		Flags:    fl(idArg("task"), idFlag),
		Examples: []string{"artcoded tasks disable"},
	},
	"tasks delete": {
		Desc:     "Deletes a task, after a confirmation.",
		Flags:    fl(idArg("task"), idFlag),
		Examples: []string{"artcoded tasks delete 66b2 --yes"},
	},
	"tasks actions": {
		Desc:     "The actions a task can run, with their default cron and parameters (* = required). Use them with tasks add --action KEY --param KEY=VALUE.",
		Examples: []string{"artcoded tasks actions"},
	},
	"tasks results": {
		Desc:     "The results of the runs of an action: status, start / end and messages, latest first.",
		Flags:    fl("<action key> | the action (or --key); chosen in a list when missing", "--key KEY | the action", pageOpts),
		Examples: []string{"artcoded tasks results DUMP", "artcoded tasks results --key DUMP --page 2"},
	},
	"tasks validate-cron": {
		Desc:     "Checks a cron expression (6 fields: second minute hour day month weekday).",
		Flags:    fl("<expression> | the expression (quote it, or several words)", "--cron EXPR | the expression"),
		Examples: []string{"artcoded tasks validate-cron \"0 30 8 * * MON-FRI\""},
	},

	// ---------------------------------------------------------------- timesheets
	"timesheets list": {
		Desc:     "The timesheets, latest month first. One per month and client, created automatically for the ongoing clients.",
		Flags:    fl("--year YEAR | only this year", "--client NAME | only the clients whose name contains this", "--open | only the timesheets not closed"),
		Examples: []string{"artcoded timesheets list --open", "artcoded timesheets list --year 2026 --client acme"},
	},
	"timesheets show": {
		Desc:     "The days of a timesheet: hours, type of day, description, project. The empty weekend days are hidden.",
		Flags:    fl(idArg("timesheet"), idFlag, "--all-days | also the empty weekend days"),
		Examples: []string{"artcoded timesheets show", "artcoded timesheets show 66c3 --all-days", "# the timesheet in the backoffice\nartcoded timesheets show 66c3 --online"},
	},
	"timesheets log": {
		Desc: `Logs a day: the hours worked, or a day off (sickness, holidays).
The timesheet is the open one of the month of the day; asked when there are several clients.
The hours become times like in the backoffice: morning from 8:00 (9:00 when the max hours per day is below 8)
until 12:00, then from 13:00. 0 hours clears the day.`,
		Flags: fl("[<hours>] | hours worked, e.g. 8 or 6,5 (or --hours); default the max hours per day",
			"--hours N | hours worked", "--date DATE | the day (default today; "+dateHelp+")",
			"--type TYPE | WORKING_DAY (default), SICKNESS, PUBLIC_HOLIDAYS, AUTHORIZED_HOLIDAYS, WEEKEND",
			"--description TEXT | what was done", "--project NAME | project (default: the one of the timesheet)",
			"--client NAME | the timesheet of this client (several open timesheets)", "--id ID | the timesheet itself"),
		Examples: []string{
			"# 8 hours today\nartcoded timesheets log 8",
			"artcoded timesheets log 6,5 --date yesterday --description \"migration\" --yes",
			"artcoded timesheets log --date 2026-11-11 --type PUBLIC_HOLIDAYS --yes",
		},
	},
	"timesheets rate": {
		Desc:     "Changes the rate of a day (clients with several rates). Allowed even on a closed timesheet.",
		Flags:    fl(idArg("timesheet"), idFlag, "--date DATE | the day (default today)", "--rate RATE_ID | the rate of the client; chosen in a list when missing"),
		Examples: []string{"artcoded timesheets rate 66c3 --date 2026-11-04"},
	},
	"timesheets settings": {
		Desc:     "The min / max hours per day of a timesheet (used by log and the estimates).",
		Flags:    fl(idArg("timesheet"), idFlag, "--min N | min hours per day", "--max N | max hours per day"),
		Examples: []string{"artcoded timesheets settings 66c3 --min 0 --max 8"},
	},
	"timesheets estimate": {
		Desc:     "The amount to invoice: this month (all clients), or of one timesheet.",
		Flags:    fl("[<id>] | a timesheet (or --id); without: this month", idFlag),
		Examples: []string{"artcoded timesheets estimate", "artcoded timesheets estimate 66c3"},
	},
	"timesheets stats": {
		Desc:     "Hours and days worked per year, month and client, and the days worked per year.",
		Flags:    fl("--year YEAR | only this year"),
		Examples: []string{"artcoded timesheets stats --year 2026"},
	},
	"timesheets close": {
		Desc:     "Closes a timesheet: its pdf is generated, the invoice can be generated.",
		Flags:    fl(idArg("timesheet"), idFlag),
		Examples: []string{"artcoded timesheets close", "artcoded timesheets close 66c3 --yes"},
	},
	"timesheets reopen": {
		Desc:     "Reopens a closed timesheet to change it.",
		Flags:    fl(idArg("timesheet"), idFlag),
		Examples: []string{"artcoded timesheets reopen 66c3"},
	},
	"timesheets invoice": {
		Desc:     "Generates the invoice of a closed timesheet (one row per rate).",
		Flags:    fl(idArg("timesheet"), idFlag),
		Examples: []string{"artcoded timesheets close 66c3 --yes && artcoded timesheets invoice 66c3 --yes"},
	},
	"timesheets download": {
		Desc:     "The pdf of a closed timesheet.",
		Flags:    fl(idArg("timesheet"), idFlag, outOpts),
		Examples: []string{"artcoded timesheets download 66c3 --open", "artcoded timesheets download --out ~/timesheets/"},
	},
	"timesheets delete": {
		Desc:     "Deletes a timesheet, after a confirmation.",
		Flags:    fl(idArg("timesheet"), idFlag),
		Examples: []string{"artcoded timesheets delete 66c3"},
	},

	// ---------------------------------------------------------------- expenses
	"expenses list": {
		Desc: `Searches the expenses, latest first. The backend lists one of three sets:
  - by default: the expenses TO PROCESS, the ones in no dossier yet;
  - --processed: the expenses already in a dossier (archived);
  - --bookmarked: the bookmarked ones, processed or not.
An empty list by default means everything is processed: try --processed.
The other options filter inside the set.`,
		Flags: fl("--processed | the expenses already in a dossier (alias --archived)", "--archived | same as --processed", "--id ID | only this expense",
			"--bookmarked | the bookmarked expenses", "--tag TAG | only this tag (see expenses labels)",
			"--subject TEXT | subject contains (case insensitive)", "--body TEXT | description contains",
			"--from DATE | received after ("+dateHelp+")", "--to DATE | received before",
			"--proof | only with a payment proof (--no-proof: only without)", pageOpts),
		Examples: []string{
			"# what is left to process\nartcoded expenses list",
			"# what was processed this year\nartcoded expenses list --processed --from 2026-01-01",
			"artcoded expenses list --processed --tag TRAVEL --size 100",
			"artcoded expenses list --no-proof",
			"# the same page in the backoffice\nartcoded expenses list --processed --online",
			"artcoded expenses list --processed --json | jq '[.content[].priceTot] | add'",
		},
	},
	"expenses show": {
		Desc:     "An expense: subject, date, tag, price, its files and payment proof.",
		Flags:    fl(idArg("expense (to process)"), idFlag),
		Examples: []string{"artcoded expenses show", "artcoded expenses show 66d4"},
	},
	"expenses add": {
		Desc: `Adds an expense from one or more files (invoice, receipt, ticket...), like an expense received by mail.
The subject defaults to the name of the first file. Then the tag and the price are asked (enter to skip).
Paths: arguments or --file (repeatable, globs work); asked one per line when missing (drag & drop works).`,
		Flags: fl("<file...> | the files", "--file PATH | a file (repeatable)", "--subject TEXT | the subject (default: the first file name)",
			"--body TEXT | a description", "--tag TAG | its tag (see expenses labels); the default price of the tag applies",
			"--price AMOUNT | price excluding VAT (100 or 100,50)", "--vat AMOUNT | VAT amount (not the rate)"),
		Examples: []string{
			"artcoded expenses add ~/Downloads/receipt.pdf",
			"artcoded expenses add ticket.pdf receipt.jpg --subject \"Train Brussels-Paris\" --tag TRAVEL --price 120 --vat 0 --yes",
			"artcoded expenses add ~/scans/*.pdf --subject \"hosting 2026\" --yes",
		},
	},
	"expenses tag": {
		Desc:     "Sets the tag (label) of expenses. The default price of the label applies to them.",
		Flags:    fl(idsArg("expenses (to process)"), idFlag, "--tag TAG | the tag; chosen in the labels when missing"),
		Examples: []string{"# choose the expenses, then the tag\nartcoded expenses tag", "artcoded expenses tag 66d4 66d5 --tag TRAVEL"},
	},
	"expenses price": {
		Desc:     "Sets the price of an expense: excluding VAT, and the VAT amount.",
		Flags:    fl(idArg("expense"), idFlag, "--price AMOUNT | price excluding VAT", "--vat AMOUNT | VAT amount"),
		Examples: []string{"artcoded expenses price 66d4 --price 99,17 --vat 20,83"},
	},
	"expenses attach": {
		Desc:     "Adds a file to an expense, e.g. the payment proof.",
		Flags:    fl(idArg("expense"), "<file> | the file (or --file)", idFlag, "--file PATH | the file", "--payment-proof | it is the payment proof"),
		Examples: []string{"artcoded expenses attach 66d4 ~/Downloads/bank-transfer.pdf --payment-proof"},
	},
	"expenses detach": {
		Desc:     "Removes a file (or the payment proof) of an expense.",
		Flags:    fl(idArg("expense"), idFlag, "--attachment UPLOAD_ID | the file; chosen in a list when missing"),
		Examples: []string{"artcoded expenses detach 66d4"},
	},
	"expenses download": {
		Desc:     "Downloads every file of an expense, payment proof included.",
		Flags:    fl(idArg("expense"), idFlag, outOpts),
		Examples: []string{"artcoded expenses download 66d4 --out ~/Desktop/"},
	},
	"expenses edit": {
		Desc:     "Edits an expense as json in the editor (subject, date, body...).",
		Flags:    fl(idArg("expense"), idFlag),
		Examples: []string{"artcoded expenses edit 66d4"},
	},
	"expenses bookmark": {
		Desc:     "Bookmarks an expense, or removes the bookmark.",
		Flags:    fl(idArg("expense"), idFlag),
		Examples: []string{"artcoded expenses bookmark 66d4"},
	},
	"expenses delete": {
		Desc:     "Deletes expenses and their files, after a confirmation.",
		Flags:    fl(idsArg("expenses (to process)"), idFlag),
		Examples: []string{"artcoded expenses delete 66d4 --yes"},
	},
	"expenses summaries": {
		Desc:     "Totals per tag: excluding VAT and VAT.",
		Examples: []string{"artcoded expenses summaries"},
	},
	"expenses labels": {
		Desc:     "The tags (labels) of the expenses, with their default price.",
		Examples: []string{"artcoded expenses labels"},
	},
	"expenses labels-edit": {
		Desc:     "Edits every label as json in the editor: name, color, default price (priceHVAT, vat), hidden.",
		Examples: []string{"artcoded expenses labels-edit"},
	},
	"expenses exceptional": {
		Desc:     "The expenses far above the usual ones (price >= factor x median and >= min), and the reviewed ones.",
		Flags:    fl("--factor N | how many times the median (default of the backend)", "--min AMOUNT | minimum price"),
		Examples: []string{"artcoded expenses exceptional", "artcoded expenses exceptional --factor 5 --min 2000"},
	},
	"expenses review": {
		Desc:     "Reviews big expenses: CONFIRMED (exceptional: taxes, one-off), DENIED (a normal expense) or NONE (not reviewed).",
		Flags:    fl(idsArg("expenses (from expenses exceptional)"), idFlag, "--status CONFIRMED|DENIED|NONE | the review; asked when missing"),
		Examples: []string{"artcoded expenses review 66d4 --status CONFIRMED"},
	},

	// ---------------------------------------------------------------- invoices
	"invoices list": {
		Desc: `The invoices, latest first. Like the expenses, one of:
  - by default: the invoices in no dossier yet (drafts, generated);
  - --archived: the ones in a dossier;
  - --deleted: the deleted ones (restorable);
  - --bookmarked: the bookmarked ones.`,
		Flags:    fl("--archived | the invoices in a dossier", "--deleted | the deleted invoices", "--bookmarked | the bookmarked invoices", pageOpts),
		Examples: []string{"artcoded invoices list", "artcoded invoices list --archived --size 50", "artcoded invoices list --demo", "artcoded invoices list --online", "artcoded invoices list --json | jq '.content[] | {invoiceNumber, total}'"},
	},
	"invoices show": {
		Desc:     "An invoice: client, rows, totals, peppol status, extra files.",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices show"},
	},
	"invoices new": {
		Desc: `Creates an invoice, guided: the client fills the "bill to" part, its rate and VAT;
then the rows (project, nature, period MM/YYYY, amount, rate), the date and a note.
--edit opens the result as json before saving. The pdf is generated in the background.`,
		Flags: fl("--client NAME | the client (name contains); chosen in a list when missing",
			"--from INVOICE_ID | a copy of this invoice", "--date DATE | date of the invoice (default today)",
			"--project NAME | project of the row", "--nature TEXT | nature of the row", "--period MM/YYYY | period of the row (default this month)",
			"--amount N | amount of the row", "--amount-type DAYS|HOURS | the amount is in", "--rate AMOUNT | rate of the row",
			"--rate-type DAYS|HOURS | the rate is per", "--hours-per-day N | when the amount and the rate differ in unit",
			"--tax-rate PERCENT | VAT rate (default the client's, else 21)", "--note TEXT | special note on the invoice",
			"--edit | review the json in the editor before saving"),
		Examples: []string{
			"artcoded invoices new",
			"artcoded invoices new --client acme --amount 18 --period 10/2026 --yes",
			"artcoded invoices new --from 66e5 --edit",
		},
	},
	"invoices edit": {
		Desc:     "Edits an invoice not generated yet as json in the editor. A generated invoice cannot change: make a credit note.",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices edit 66e5"},
	},
	"invoices download": {
		Desc:     "The pdf of an invoice, or its peppol UBL xml.",
		Flags:    fl(idArg("invoice"), idFlag, "--xml | the peppol xml instead of the pdf", outOpts),
		Examples: []string{"artcoded invoices download --open", "artcoded invoices download 66e5 --xml --out invoice.xml"},
	},
	"invoices upload": {
		Desc:     "Uploads the pdf of an invoice made elsewhere (manual invoice).",
		Flags:    fl(idArg("invoice"), "<file> | the pdf (or --file)", idFlag, "--file PATH | the pdf"),
		Examples: []string{"artcoded invoices upload 66e5 ~/invoice-2026-012.pdf"},
	},
	"invoices attachments": {
		Desc:     "The extra files of a generated invoice, embedded in its peppol xml (generated again when not sent yet).",
		Flags:    fl(idArg("invoice"), idFlag, "--add PATH | a file to add (repeatable)", "--remove UPLOAD_ID | a file to remove (repeatable)", "--op OPERATION | asked: add files, remove a file, nothing"),
		Examples: []string{"artcoded invoices attachments 66e5", "artcoded invoices attachments 66e5 --add timesheet.pdf --add contract.pdf"},
	},
	"invoices credit-note": {
		Desc:     "Makes the credit note of an invoice (archived, not already credited).",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices credit-note 66e5"},
	},
	"invoices peppol-validate": {
		Desc:     "Validates the peppol xml of an invoice and prints the problems.",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices peppol-validate 66e5"},
	},
	"invoices peppol-send": {
		Desc:     "Sends an invoice to peppol, after a confirmation.",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices peppol-validate 66e5 && artcoded invoices peppol-send 66e5 --yes"},
	},
	"invoices bookmark": {
		Desc:     "Bookmarks an invoice, or removes the bookmark.",
		Flags:    fl(idArg("invoice"), idFlag),
		Examples: []string{"artcoded invoices bookmark 66e5"},
	},
	"invoices delete": {
		Desc:     "Deletes an invoice: restorable (invoices restore), or for good with --physical.",
		Flags:    fl(idArg("invoice"), idFlag, "--physical | delete for good"),
		Examples: []string{"artcoded invoices delete 66e5", "artcoded invoices delete 66e5 --physical --yes"},
	},
	"invoices restore": {
		Desc:     "Restores a deleted invoice.",
		Flags:    fl(idArg("deleted invoice"), idFlag),
		Examples: []string{"artcoded invoices restore"},
	},
	"invoices summaries": {
		Desc:     "Amounts invoiced per period and client.",
		Examples: []string{"artcoded invoices summaries"},
	},
	"invoices templates": {
		Desc:     "The invoice templates (same as templates list --type INVOICE).",
		Examples: []string{"artcoded invoices templates"},
	},

	// ---------------------------------------------------------------- dossiers
	"dossiers active": {
		Desc:     "The active dossier (the one being filled) with its expenses, invoices and documents.",
		Examples: []string{"artcoded dossiers active"},
	},
	"dossiers list": {
		Desc:     "The dossiers: the open ones by default, --closed for the closed ones.",
		Flags:    fl("--closed | the closed dossiers", "--bookmarked | the bookmarked dossiers", pageOpts),
		Examples: []string{"artcoded dossiers list --closed"},
	},
	"dossiers show": {
		Desc:     "A dossier: content, VAT, totals.",
		Flags:    fl(idArg("dossier"), idFlag),
		Examples: []string{"artcoded dossiers show"},
	},
	"dossiers new": {
		Desc:     "Opens a new dossier (one at a time is active).",
		Flags:    fl("<name> | the name (or --name; default YYYY-MM)", "--name NAME | the name", "--description TEXT | a description", "--vat-due AMOUNT | VAT due", "--from-previous | made from the previous dossier"),
		Examples: []string{"artcoded dossiers new 2026-Q4", "artcoded dossiers new --from-previous"},
	},
	"dossiers edit": {
		Desc:     "Edits the active dossier as json in the editor (name, VAT, advance payments...).",
		Examples: []string{"artcoded dossiers edit"},
	},
	"dossiers add-expenses": {
		Desc:     "Puts expenses in the active dossier: they become processed. Only tagged expenses can be added.",
		Flags:    fl(idsArg("expenses to process"), idFlag, "--tag TAG | only offer the expenses of this tag"),
		Examples: []string{"# choose: 1,3-7 or all\nartcoded dossiers add-expenses", "artcoded dossiers add-expenses --tag TRAVEL"},
	},
	"dossiers remove-expense": {
		Desc:     "Takes an expense out of the active dossier.",
		Flags:    fl(idArg("expense of the active dossier"), idFlag),
		Examples: []string{"artcoded dossiers remove-expense 66d4"},
	},
	"dossiers add-invoice": {
		Desc:     "Puts a generated invoice in the active dossier.",
		Flags:    fl(idArg("generated invoice"), idFlag),
		Examples: []string{"artcoded dossiers add-invoice"},
	},
	"dossiers remove-invoice": {
		Desc:     "Takes an invoice out of the active dossier.",
		Flags:    fl(idArg("invoice of the active dossier"), idFlag),
		Examples: []string{"artcoded dossiers remove-invoice 66e5"},
	},
	"dossiers add-document": {
		Desc:     "Puts an administrative document in the active dossier.",
		Flags:    fl(idArg("document"), idFlag),
		Examples: []string{"artcoded dossiers add-document"},
	},
	"dossiers remove-document": {
		Desc:     "Takes a document out of the active dossier.",
		Flags:    fl(idArg("document of the active dossier"), idFlag),
		Examples: []string{"artcoded dossiers remove-document"},
	},
	"dossiers close": {
		Desc:     "Closes the active dossier: zipped and sent to the accountant. After a confirmation.",
		Examples: []string{"artcoded dossiers close"},
	},
	"dossiers recall": {
		Desc:     "Reopens a closed dossier for modification.",
		Flags:    fl(idArg("closed dossier"), idFlag),
		Examples: []string{"artcoded dossiers recall"},
	},
	"dossiers delete-active": {
		Desc:     "Deletes the active dossier, after a confirmation.",
		Examples: []string{"artcoded dossiers delete-active"},
	},
	"dossiers download": {
		Desc:     "The zip of a closed dossier.",
		Flags:    fl(idArg("dossier"), idFlag, outOpts),
		Examples: []string{"artcoded dossiers download --out ~/accounting/"},
	},
	"dossiers summary-pdf": {
		Desc:     "The summary pdf of a dossier.",
		Flags:    fl(idArg("dossier"), idFlag, outOpts),
		Examples: []string{"artcoded dossiers summary-pdf 66f6 --open"},
	},
	"dossiers report": {
		Desc:     "The pdf report of a dossier compared with the previous years: every month, or one month.",
		Flags:    fl(idArg("dossier"), idFlag, "--month YYYY-MM | only this month", outOpts),
		Examples: []string{"artcoded dossiers report 66f6 --open", "artcoded dossiers report 66f6 --month 2026-10"},
	},
	"dossiers annual-report": {
		Desc:     "The pdf annual report: every dossier of the year compared with the other years, and the projection.",
		Flags:    fl("<year> | the year (or --year; default this year)", "--year YEAR | the year", outOpts),
		Examples: []string{"artcoded dossiers annual-report 2026 --open"},
	},
	"dossiers summaries": {
		Desc:     "Totals of expenses and earnings per dossier.",
		Flags:    fl("--closed | the closed dossiers"),
		Examples: []string{"artcoded dossiers summaries --closed"},
	},
	"dossiers by-year": {
		Desc:     "The closed dossiers grouped by year.",
		Examples: []string{"artcoded dossiers by-year"},
	},
	"dossiers size": {
		Desc:     "The size of the files of a dossier.",
		Flags:    fl(idArg("dossier"), idFlag),
		Examples: []string{"artcoded dossiers size 66f6"},
	},
	"dossiers bookmark": {
		Desc:     "Bookmarks a dossier, or removes the bookmark.",
		Flags:    fl(idArg("dossier"), idFlag),
		Examples: []string{"artcoded dossiers bookmark 66f6"},
	},
	"dossiers import": {
		Desc:     "Imports a dossier from a zip (see import-example for the xlsx inside).",
		Flags:    fl("<zip> | the zip (or --file)", "--file PATH | the zip"),
		Examples: []string{"artcoded dossiers import ~/dossier-2019.zip"},
	},
	"dossiers import-example": {
		Desc:     "Downloads the xlsx example of an import.",
		Flags:    fl(outOpts),
		Examples: []string{"artcoded dossiers import-example --open"},
	},

	// ---------------------------------------------------------------- documents
	"documents list": {
		Desc:     "Searches the administrative documents, latest first.",
		Flags:    fl("--title TEXT | title contains", "--description TEXT | description contains", "--tag TAG | has this tag (repeatable)", "--from DATE | added after", "--to DATE | added before", "--bookmarked | the bookmarked ones", pageOpts),
		Examples: []string{"artcoded documents list --tag legal", "artcoded documents list --title statuts"},
	},
	"documents show": {
		Desc:     "A document and its file.",
		Flags:    fl(idArg("document"), idFlag),
		Examples: []string{"artcoded documents show"},
	},
	"documents upload": {
		Desc:     "Uploads a document. The title defaults to the file name.",
		Flags:    fl("<file> | the file (or --file)", "--file PATH | the file", "--title TEXT | the title", "--description TEXT | a description", "--tags A,B | comma separated tags"),
		Examples: []string{"artcoded documents upload ~/scan/statuts.pdf --tags legal,company", "artcoded documents upload contract.pdf --title \"lease 2027\" --yes"},
	},
	"documents edit": {
		Desc:     "Changes the title, description or tags of a document, or replaces its file.",
		Flags:    fl(idArg("document"), idFlag, "--title TEXT | the title", "--description TEXT | a description", "--tags A,B | the tags (replace them)", "--file PATH | a new file"),
		Examples: []string{"artcoded documents edit 6701 --tags legal,archive --yes"},
	},
	"documents download": {
		Desc:     "Downloads the file of a document.",
		Flags:    fl(idArg("document"), idFlag, outOpts),
		Examples: []string{"artcoded documents download --open"},
	},
	"documents bookmark": {
		Desc:     "Bookmarks a document, or removes the bookmark.",
		Flags:    fl(idArg("document"), idFlag),
		Examples: []string{"artcoded documents bookmark 6701"},
	},
	"documents delete": {
		Desc:     "Deletes a document, after a confirmation.",
		Flags:    fl(idArg("document"), idFlag),
		Examples: []string{"artcoded documents delete 6701"},
	},

	// ---------------------------------------------------------------- reports
	"reports list": {
		Desc:     "Searches the activity reports, latest updated first. ✉ counts the unread messages.",
		Flags:    fl("--title TEXT | title contains", "--content TEXT | content contains", "--tag TAG | has this tag", "--status STATUS | DRAFT, IN_PROGRESS, PENDING, DONE, CANCELLED", "--priority PRIORITY | LOW, MEDIUM, HIGH, URGENT", "--bookmarked | the bookmarked ones", pageOpts),
		Examples: []string{"artcoded reports list --status IN_PROGRESS"},
	},
	"reports show": {
		Desc:     "A report: content, post-its, files.",
		Flags:    fl(idArg("report"), idFlag),
		Examples: []string{"artcoded reports show"},
	},
	"reports new": {
		Desc:     "Writes a new report. The content (markdown) opens in the editor unless given.",
		Flags:    fl("--title TEXT | the title", "--description TEXT | a short description", "--content TEXT | the content (markdown)", "--content-file FILE | the content from a file (- : standard input)", "--tags A,B | tags", "--status STATUS | DRAFT (default), IN_PROGRESS, PENDING, DONE, CANCELLED", "--priority PRIORITY | LOW, MEDIUM (default), HIGH, URGENT", "--due DATE | due date (default in a month)", "--author EMAIL | author (default you)", "--cover IMAGE | a cover image"),
		Examples: []string{"artcoded reports new", "artcoded reports new --title \"Q4 migration\" --description \"status\" --content-file notes.md --tags migration --yes"},
	},
	"reports edit": {
		Desc:     "Edits a report: the current values are the defaults, the content opens in the editor.",
		Flags:    fl(idArg("report"), idFlag, "--title TEXT | the title", "--description TEXT | a short description", "--content TEXT | the content", "--content-file FILE | the content from a file", "--tags A,B | tags", "--status STATUS | the status", "--priority PRIORITY | the priority", "--due DATE | due date", "--author EMAIL | author", "--cover IMAGE | a new cover"),
		Examples: []string{"artcoded reports edit 6702 --status DONE --yes"},
	},
	"reports attach": {
		Desc:     "Adds files to a report.",
		Flags:    fl(idArg("report"), "<file...> | the files (or --file)", idFlag, "--file PATH | a file (repeatable)"),
		Examples: []string{"artcoded reports attach 6702 diagram.png notes.pdf"},
	},
	"reports detach": {
		Desc:     "Removes a file of a report.",
		Flags:    fl(idArg("report"), idFlag, "--attachment UPLOAD_ID | the file; chosen in a list when missing"),
		Examples: []string{"artcoded reports detach 6702"},
	},
	"reports processed": {
		Desc:     "Marks a file of a report processed, or not processed again.",
		Flags:    fl(idArg("report"), idFlag, "--attachment UPLOAD_ID | the file; chosen in a list when missing"),
		Examples: []string{"artcoded reports processed 6702"},
	},
	"reports download": {
		Desc:     "Downloads the files of a report.",
		Flags:    fl(idArg("report"), idFlag, outOpts),
		Examples: []string{"artcoded reports download 6702 --out ~/Desktop/"},
	},
	"reports pdf": {
		Desc:     "The pdf of a report.",
		Flags:    fl(idArg("report"), idFlag, outOpts),
		Examples: []string{"artcoded reports pdf 6702 --open"},
	},
	"reports todos": {
		Desc:     "Edits the post-its of a report (todos / inProgress / done) as json in the editor: move the items between the lists.",
		Flags:    fl(idArg("report"), idFlag),
		Examples: []string{"artcoded reports todos 6702"},
	},
	"reports messages": {
		Desc:     "The messages of a report (marked read).",
		Flags:    fl(idArg("report"), idFlag),
		Examples: []string{"artcoded reports messages 6702"},
	},
	"reports post": {
		Desc:     "Posts a message on a report, with files.",
		Flags:    fl(idArg("report"), idFlag, "--message TEXT | the message (editor when missing)", "--message-file FILE | the message from a file", "--file PATH | a file (repeatable)"),
		Examples: []string{"artcoded reports post 6702 --message \"done, see the log\" --file build.log"},
	},
	"reports unread": {
		Desc:     "Unread messages per status and subscriber.",
		Examples: []string{"artcoded reports unread"},
	},
	"reports tags": {
		Desc:     "The tags used by the reports.",
		Examples: []string{"artcoded reports tags"},
	},
	"reports bookmark": {
		Desc:     "Bookmarks a report, or removes the bookmark.",
		Flags:    fl(idArg("report"), idFlag),
		Examples: []string{"artcoded reports bookmark 6702"},
	},
	"reports delete": {
		Desc:     "Deletes a report, after a confirmation.",
		Flags:    fl(idArg("report"), idFlag),
		Examples: []string{"artcoded reports delete 6702"},
	},

	// ---------------------------------------------------------------- gallery
	"gallery list": {
		Desc:     "The images of the public gallery.",
		Flags:    fl(pageOpts),
		Examples: []string{"artcoded gallery list"},
	},
	"gallery add": {
		Desc:     "Adds an image to the gallery (thumbnails are made in the background).",
		Flags:    fl("<image> | the image (or --file)", "--file PATH | the image", "--title TEXT | the title (default: the file name)", "--description TEXT | a description", "--visible | visible in the public gallery (default yes; --no-visible)", "--date DATE | visible from"),
		Examples: []string{"artcoded gallery add ~/Pictures/sunset.jpg --title Sunset --yes"},
	},
	"gallery edit": {
		Desc:     "Changes the title, description, visibility or the image.",
		Flags:    fl(idArg("image"), idFlag, "--title TEXT | the title", "--description TEXT | a description", "--visible | visible (--no-visible: hidden)", "--date DATE | visible from", "--file PATH | a new image"),
		Examples: []string{"artcoded gallery edit 6703 --no-visible --yes"},
	},
	"gallery download": {
		Desc:     "Downloads an image.",
		Flags:    fl(idArg("image"), idFlag, outOpts),
		Examples: []string{"artcoded gallery download 6703 --open"},
	},
	"gallery delete": {
		Desc:     "Deletes an image, after a confirmation.",
		Flags:    fl(idArg("image"), idFlag),
		Examples: []string{"artcoded gallery delete 6703"},
	},

	// ---------------------------------------------------------------- cv
	"cv show": {
		Desc:     "The curriculum: person, experiences, skills.",
		Examples: []string{"artcoded cv show", "artcoded cv show --json > cv.json"},
	},
	"cv edit": {
		Desc:     "Edits the whole curriculum as json in the editor.",
		Examples: []string{"artcoded cv edit"},
	},
	"cv download": {
		Desc:     "The pdf of the curriculum (default cv template).",
		Flags:    fl(outOpts),
		Examples: []string{"artcoded cv download --open"},
	},
	"cv requests": {
		Desc:     "The download requests of the cv (who asked it).",
		Examples: []string{"artcoded cv requests"},
	},
	"cv request-delete": {
		Desc:     "Deletes download requests.",
		Flags:    fl(idsArg("requests"), idFlag),
		Examples: []string{"artcoded cv request-delete"},
	},
	"cv templates": {
		Desc:     "The cv templates (same as templates list --type CV).",
		Examples: []string{"artcoded cv templates"},
	},

	// ---------------------------------------------------------------- finance
	"finance list": {
		Desc:     "The portfolios.",
		Examples: []string{"artcoded finance list"},
	},
	"finance show": {
		Desc:     "A portfolio with the current prices: 5 days evolution and since added.",
		Flags:    fl(idArg("portfolio"), idFlag),
		Examples: []string{"artcoded finance show"},
	},
	"finance new": {
		Desc:     "Creates a portfolio.",
		Flags:    fl("<name> | the name (or --name)", "--name NAME | the name", "--principal | the principal portfolio"),
		Examples: []string{"artcoded finance new ETFs --principal"},
	},
	"finance rename": {
		Desc:     "Renames a portfolio, or makes it the principal one.",
		Flags:    fl(idArg("portfolio"), idFlag, "--name NAME | the new name", "--principal | principal (--no-principal: not)"),
		Examples: []string{"artcoded finance rename 6704 --name Stocks"},
	},
	"finance add-tick": {
		Desc:     "Adds a symbol to a portfolio; the price when added defaults to the current one.",
		Flags:    fl(idArg("portfolio"), idFlag, "--symbol SYMBOL | e.g. AAPL, IWDA.AS (see finance search)", "--price AMOUNT | price when added", "--currency CODE | currency (default the quote's)"),
		Examples: []string{"artcoded finance add-tick 6704 --symbol IWDA.AS"},
	},
	"finance remove-tick": {
		Desc:     "Removes a symbol from a portfolio.",
		Flags:    fl(idArg("portfolio"), idFlag, "--symbol SYMBOL | the symbol; chosen when missing"),
		Examples: []string{"artcoded finance remove-tick 6704 --symbol AAPL"},
	},
	"finance delete": {
		Desc:     "Deletes a portfolio, after a confirmation.",
		Flags:    fl(idArg("portfolio"), idFlag),
		Examples: []string{"artcoded finance delete 6704"},
	},
	"finance quote": {
		Desc:     "The current price of symbols and their evolution on the range.",
		Flags:    fl("<symbol...> | symbols (or --symbol)", "--symbol SYMBOL | a symbol", "--range RANGE | 1d, 5d (default), 1mo, 6mo, 1y, 5y", "--interval INTERVAL | 1d (default), 1wk..."),
		Examples: []string{"artcoded finance quote AAPL MSFT IWDA.AS", "artcoded finance quote BTC-EUR --range 1mo"},
	},
	"finance search": {
		Desc:     "Finds symbols by name.",
		Flags:    fl("<query> | what to search (or --q)", "--q TEXT | what to search"),
		Examples: []string{"artcoded finance search msci world"},
	},

	// ---------------------------------------------------------------- uploads
	"uploads list": {
		Desc:     "Every file of the application, latest first, with what it belongs to.",
		Flags:    fl("--name TEXT | file name contains", "--correlation-id ID | the files of this entity (an invoice, an expense...)", "--public | only the public files (--no-public: the private ones)", "--bookmarked | the bookmarked ones", "--from DATE | uploaded after", "--to DATE | uploaded before", pageOpts),
		Examples: []string{"artcoded uploads list --name invoice", "artcoded uploads list --correlation-id 66e5"},
	},
	"uploads show": {
		Desc:     "A file: name, type, size, owner; the public url for a public file.",
		Flags:    fl(idArg("file"), idFlag),
		Examples: []string{"artcoded uploads show 6705"},
	},
	"uploads upload": {
		Desc:     "Uploads files (not linked to anything unless --correlation-id).",
		Flags:    fl("<file...> | the files (or --file)", "--file PATH | a file (repeatable)", "--public | public: downloadable without login", "--correlation-id ID | link them to this entity"),
		Examples: []string{"artcoded uploads upload a.pdf b.pdf", "artcoded uploads upload logo.png --public --json"},
	},
	"uploads download": {
		Desc:     "Downloads files: one by one, or one zip with --zip.",
		Flags:    fl(idsArg("files"), idFlag, "--zip | several files in one zip", outOpts),
		Examples: []string{"artcoded uploads download 6705 --open", "artcoded uploads download 6705 6706 --zip --out files.zip"},
	},
	"uploads cat": {
		Desc:     "Prints a file to the standard output.",
		Flags:    fl(idArg("file"), idFlag),
		Examples: []string{"artcoded uploads cat 6705 | less"},
	},
	"uploads search": {
		Desc:     "Searches INSIDE the files (full text index: pdf, spreadsheets, documents, images ocr...).",
		Flags:    fl("<query> | what to search (or --q)", "--q TEXT | what to search", "--mode MODE | smart (default), exact, prefix, fuzzy, phrase, regex, query", "--kinds LIST | pdf,spreadsheet,csv,document,image,text", "--extensions LIST | e.g. pdf,xlsx", "--fields LIST | content,file_name,sheet,context", "--from DATE | modified after (YYYY-MM-DD)", "--to DATE | modified before", "--fuzziness N | for --mode fuzzy", pageOpts),
		Examples: []string{"artcoded uploads search \"BE0123 456 789\"", "artcoded uploads search invoice --kinds pdf --mode prefix"},
	},
	"uploads index-stats": {
		Desc:     "The state of the full text index.",
		Examples: []string{"artcoded uploads index-stats"},
	},
	"uploads bookmark": {
		Desc:     "Bookmarks a file, or removes the bookmark.",
		Flags:    fl(idArg("file"), idFlag),
		Examples: []string{"artcoded uploads bookmark 6705"},
	},
	"uploads links": {
		Desc:     "What the files belong to: correlation id -> label.",
		Examples: []string{"artcoded uploads links"},
	},
	"uploads delete": {
		Desc:     "Deletes files, after a confirmation. What uses them loses them.",
		Flags:    fl(idsArg("files"), idFlag),
		Examples: []string{"artcoded uploads delete 6705"},
	},

	// ---------------------------------------------------------------- sparql
	"sparql query": {
		Desc:     "Runs a sparql query (or update) on the triplestore. The query: argument, --file, or the editor. CONSTRUCT / DESCRIBE answer turtle.",
		Flags:    fl("[<query>] | the query", "--file FILE | the query from a file", "--query TEXT | the query", "--query-file FILE | the query from a file (- : standard input)", "--format FORMAT | table (default), json, csv, tsv, xml, turtle, ntriples"),
		Examples: []string{"artcoded sparql query \"SELECT * WHERE { ?s ?p ?o } LIMIT 10\"", "artcoded sparql query --file q.rq --format csv > out.csv"},
	},

	// ---------------------------------------------------------------- prospects
	"prospects list": {
		Desc:     "The messages of the contact form.",
		Examples: []string{"artcoded prospects list"},
	},
	"prospects show": {
		Desc:     "A message of the contact form.",
		Flags:    fl(idArg("message"), idFlag),
		Examples: []string{"artcoded prospects show"},
	},
	"prospects delete": {
		Desc:     "Deletes messages, after a confirmation.",
		Flags:    fl(idsArg("messages"), idFlag),
		Examples: []string{"artcoded prospects delete --yes 6706"},
	},

	// ---------------------------------------------------------------- clients
	"clients list": {
		Desc:     "The billable clients: rate, contract status, dates.",
		Flags:    fl("--status STATUS | NOT_STARTED_YET, ONGOING, DONE"),
		Examples: []string{"artcoded clients list --status ONGOING"},
	},
	"clients show": {
		Desc:     "A client: details, rates, documents.",
		Flags:    fl(idArg("client"), idFlag),
		Examples: []string{"artcoded clients show"},
	},
	"clients add": {
		Desc:     "Adds a client, guided. Several rates: clients edit afterwards.",
		Flags:    fl("--name NAME | the name", "--project NAME | the project", "--nature TEXT | nature of the work (invoice rows)", "--vat NUMBER | VAT number", "--address TEXT | address", "--city TEXT | postcode and city", "--country CODE | country (default BE)", "--email EMAIL | email (invoices)", "--phone NUMBER | phone", "--rate AMOUNT | the rate", "--rate-type DAYS|HOURS | per day (default) or hour", "--tax-rate PERCENT | VAT rate (default 21)", "--days-to-pay N | days to pay (default 30)", "--status STATUS | NOT_STARTED_YET, ONGOING (default), DONE", "--start DATE | contract start", "--end DATE | contract end", "--working-days LIST | MONDAY,TUESDAY... (default monday to friday)"),
		Examples: []string{"artcoded clients add", "artcoded clients add --name ACME --project Portal --vat BE0123456789 --address \"Rue X 1\" --city \"1000 Brussels\" --email ap@acme.be --rate 650 --yes"},
	},
	"clients edit": {
		Desc:     "Edits a client as json in the editor (rates, working days...).",
		Flags:    fl(idArg("client"), idFlag),
		Examples: []string{"artcoded clients edit"},
	},
	"clients status": {
		Desc:     "Changes the contract status of a client.",
		Flags:    fl(idArg("client"), idFlag, "--status STATUS | NOT_STARTED_YET, ONGOING, DONE"),
		Examples: []string{"artcoded clients status 6707 --status DONE"},
	},
	"clients upload": {
		Desc:     "Adds a document (contract, purchase order...) to a client.",
		Flags:    fl(idArg("client"), "<file> | the file (or --file)", idFlag, "--file PATH | the file"),
		Examples: []string{"artcoded clients upload 6707 contract.pdf"},
	},
	"clients documents": {
		Desc:     "The documents of a client.",
		Flags:    fl(idArg("client"), idFlag),
		Examples: []string{"artcoded clients documents 6707"},
	},
	"clients download": {
		Desc:     "Downloads a document of a client, or all with --all.",
		Flags:    fl(idArg("client"), idFlag, "--attachment UPLOAD_ID | the document; chosen in a list when missing", "--all | every document", outOpts),
		Examples: []string{"artcoded clients download 6707 --all --out ~/acme/"},
	},
	"clients remove-document": {
		Desc:     "Removes a document of a client.",
		Flags:    fl(idArg("client"), idFlag, "--attachment UPLOAD_ID | the document; chosen in a list when missing"),
		Examples: []string{"artcoded clients remove-document 6707"},
	},
	"clients delete": {
		Desc:     "Deletes a client, after a confirmation.",
		Flags:    fl(idArg("client"), idFlag),
		Examples: []string{"artcoded clients delete 6707"},
	},

	// ---------------------------------------------------------------- mails
	"mails list": {
		Desc:     "The mails sent and scheduled, latest first.",
		Flags:    fl(pageOpts),
		Examples: []string{"artcoded mails list"},
	},
	"mails show": {
		Desc:     "A mail: recipients, state, body, files.",
		Flags:    fl(idArg("mail"), idFlag),
		Examples: []string{"artcoded mails show"},
	},
	"mails send": {
		Desc:     "Sends a mail (through the mail template of the backend). Files from paths are uploaded first. After a confirmation.",
		Flags:    fl("--to EMAILS | recipients, comma separated", "--subject TEXT | subject", "--body TEXT | body (editor when missing)", "--body-file FILE | body from a file (- : standard input)", "--file PATH | an attachment (repeatable)", "--upload-id ID | an already uploaded file (repeatable)", "--bcc | the recipients in bcc", "--at DATE | send later (\"YYYY-MM-DD HH:MM\")"),
		Examples: []string{"artcoded mails send --to ap@acme.be --subject \"Invoice 2026-012\" --body-file mail.txt --file invoice.pdf", "artcoded mails send --to a@b.be,c@d.be --bcc --subject news --body hello --yes"},
	},
	"mails retry": {
		Desc:     "Sends a failed mail again.",
		Flags:    fl(idArg("mail"), idFlag),
		Examples: []string{"artcoded mails retry 6708"},
	},
	"mails edit": {
		Desc:     "Edits a scheduled mail as json in the editor.",
		Flags:    fl(idArg("mail"), idFlag),
		Examples: []string{"artcoded mails edit 6708"},
	},
	"mails delete": {
		Desc:     "Deletes a mail, after a confirmation.",
		Flags:    fl(idArg("mail"), idFlag),
		Examples: []string{"artcoded mails delete 6708"},
	},

	// ---------------------------------------------------------------- templates
	"templates list": {
		Desc:     "Every template: per type (invoice, cv, email...) one default, the one rendered. v1 FreeMarker, v2 Jinja.",
		Flags:    fl("--type TYPE | INVOICE, CV, TIMESHEET, PEPPOL_INVOICE, EMAIL, SMS... (see templates types)", "--version V1|V2 | only FreeMarker (V1) or Jinja (V2)"),
		Examples: []string{"artcoded templates list --type INVOICE"},
	},
	"templates types": {
		Desc:     "The template types: what they render and the file extension of a v2 template.",
		Examples: []string{"artcoded templates types"},
	},
	"templates show": {
		Desc:     "A template and the variables it reads (v2).",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list"),
		Examples: []string{"artcoded templates show"},
	},
	"templates edit": {
		Desc: `Edits a Jinja (v2) template in the editor, then: preview (with mock data, nothing saved), save,
edit again, edit the preview data, or abandon. Saving overwrites it: what uses it renders with the new version.`,
		Flags:    fl(idArg("v2 template"), idFlag, "--type TYPE | offer only this type in the list", "--context FILE | json data for the preview instead of the mock", "--no-preview | no preview step", "--no-open | do not open the preview, print its path"),
		Examples: []string{"artcoded templates edit", "EDITOR=\"code --wait\" artcoded templates edit 6709 --context ctx.json"},
	},
	"templates preview": {
		Desc:     "Renders a template with mock data (built from its variables) or given data, without saving anything. A text rendering is printed; a pdf / html is opened.",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list", "--file PATH | render this source instead of the saved one (a draft)", "--context FILE | json data instead of the mock", "--edit-context | edit the data in the editor first", "--no-open | do not open the preview", outOpts),
		Examples: []string{"artcoded templates preview 6709", "artcoded templates mock 6709 > ctx.json\nartcoded templates preview 6709 --file draft.html --context ctx.json --out preview.pdf --open"},
	},
	"templates mock": {
		Desc:     "Prints the mock data a preview uses (json), to change it and pass it with --context.",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list", "--file PATH | the mock of this source instead"),
		Examples: []string{"artcoded templates mock 6709 > ctx.json", "artcoded templates mock --file draft.html"},
	},
	"templates download": {
		Desc:     "The source of a template (printed when the output is not a terminal).",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list", outOpts),
		Examples: []string{"artcoded templates download 6709 --out invoice.html", "artcoded templates download 6709 | grep invoice.total"},
	},
	"templates add": {
		Desc:     "Adds a template from a file: Jinja (v2) by default, FreeMarker with --v1 (or a .ftl file).",
		Flags:    fl("<file> | the template (or --file)", "--file PATH | the template", "--type TYPE | its type", "--name NAME | name (default the file name)", "--description TEXT | a description", "--v1 | FreeMarker", "--default | make it the default of its type"),
		Examples: []string{"artcoded templates add invoice-2028.html --type INVOICE --default --yes"},
	},
	"templates replace": {
		Desc:     "Replaces the file of a template, after a confirmation.",
		Flags:    fl(idArg("template"), "<file> | the new file (or --file)", idFlag, "--type TYPE | offer only this type in the list", "--file PATH | the new file", "--v1 | it is FreeMarker (default: the current version)"),
		Examples: []string{"artcoded templates replace 6709 invoice.html --yes"},
	},
	"templates default": {
		Desc:     "Makes a template the default of its type: the one rendered.",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list"),
		Examples: []string{"artcoded templates default 6709"},
	},
	"templates delete": {
		Desc:     "Deletes a template (not the default one), after a confirmation.",
		Flags:    fl(idArg("template"), idFlag, "--type TYPE | offer only this type in the list"),
		Examples: []string{"artcoded templates delete 6709"},
	},

	// ---------------------------------------------------------------- admin
	"admin info": {
		Desc:     "Backend version and health (database, disk, mail...).",
		Examples: []string{"artcoded admin info"},
	},
	"admin logs": {
		Desc:     "The log file of the backend: the last lines, optionally filtered.",
		Flags:    fl("--tail N | number of lines (default 200, 0: all)", "--grep TEXT | only the lines containing this"),
		Examples: []string{"artcoded admin logs --grep ERROR --tail 50"},
	},
	"admin company": {
		Desc:     "The company info printed on the invoices: name, VAT, bank, accountants.",
		Examples: []string{"artcoded admin company"},
	},
	"admin company-edit": {
		Desc:     "Edits the company info: each field asked with its current value; accountants as json; images from paths.",
		Flags:    fl("--name TEXT | organization name", "--ceofullname TEXT | CEO full name", "--vatnumber TEXT | VAT number", "--address TEXT | address", "--postcode TEXT | post code", "--city TEXT | city", "--countrycode TEXT | country code", "--emailaddress TEXT | email", "--phonenumber TEXT | phone", "--secondphonenumber TEXT | second phone", "--bankaccount TEXT | IBAN", "--bankbic TEXT | BIC", "--maxdaystopay N | days to pay", "--financecharge PERCENT | finance charge", "--note TEXT | note on the invoices", "--demo | demo mode (--no-demo)", "--logo IMAGE | new logo", "--signature IMAGE | new signature", "--initial IMAGE | new initials"),
		Examples: []string{"artcoded admin company-edit", "artcoded admin company-edit --logo logo.png --yes"},
	},
	"admin menu": {
		Desc:     "The menu of the backoffice (also the one of artcoded menu): order, shown or hidden, roles, cli module.",
		Examples: []string{"artcoded admin menu"},
	},
	"admin menu-toggle": {
		Desc:     "Shows / hides menu entries.",
		Flags:    fl(idsArg("menu entries"), idFlag),
		Examples: []string{"artcoded admin menu-toggle"},
	},
	"admin menu-edit": {
		Desc:     "Edits the whole menu as json in the editor, then replaces it (the backend logs a backup).",
		Examples: []string{"artcoded admin menu-edit"},
	},
	"admin caches": {
		Desc:     "The caches of the backend.",
		Examples: []string{"artcoded admin caches"},
	},
	"admin cache-clear": {
		Desc:     "Clears caches: by name, chosen in a list, or all.",
		Flags:    fl("<name...> | caches to clear", "--all | every cache"),
		Examples: []string{"artcoded admin cache-clear --all", "artcoded admin cache-clear cvPdf"},
	},
	"admin scripts": {
		Desc:     "The scripts loaded in the backend.",
		Examples: []string{"artcoded admin scripts"},
	},
	"admin script-run": {
		Desc:     "Runs a script on the backend (javascript); prints its result. Source: --file, the arguments, or the editor.",
		Flags:    fl("[<code>] | the script", "--file FILE | the script from a file"),
		Examples: []string{"artcoded admin script-run --file report.js"},
	},
	"admin user-scripts": {
		Desc:     "The user scripts.",
		Examples: []string{"artcoded admin user-scripts"},
	},
	"admin user-script-save": {
		Desc:     "Adds or edits a user script (editor, or --file).",
		Flags:    fl("[<id>] | the script to edit (or --id); chosen when missing, new when none", idFlag, "--file FILE | the content from a file"),
		Examples: []string{"artcoded admin user-script-save --file hook.js", "artcoded admin user-script-save 670a"},
	},
	"admin user-script-delete": {
		Desc:     "Deletes a user script, after a confirmation.",
		Flags:    fl(idArg("user script"), idFlag),
		Examples: []string{"artcoded admin user-script-delete 670a"},
	},
	"admin script-bindings": {
		Desc:     "What a script can use (json).",
		Examples: []string{"artcoded admin script-bindings"},
	},
	"admin sms": {
		Desc:     "Sends a sms.",
		Flags:    fl("--to NUMBER | phone number (+32...)", "--message TEXT | the message"),
		Examples: []string{"artcoded admin sms --to +32470000000 --message \"backup done\""},
	},
	"admin dumps": {
		Desc:     "The database dumps.",
		Flags:    fl("--snapshot | the snapshots"),
		Examples: []string{"artcoded admin dumps"},
	},
	"admin notifications": {
		Desc:     "Same as home notifications.",
		Flags:    fl("--unseen | only the unseen ones"),
		Examples: []string{"artcoded admin notifications --unseen"},
	},
	"admin notifications-seen": {
		Desc:     "Same as home notifications-seen.",
		Flags:    fl(idsArg("notifications"), idFlag, "--all | every notification", "--seen BOOL | false to mark them unseen"),
		Examples: []string{"artcoded admin notifications-seen --all"},
	},
	"admin notifications-delete": {
		Desc:     "Same as home notifications-delete.",
		Flags:    fl(idsArg("notifications"), idFlag, "--all | every seen notification"),
		Examples: []string{"artcoded admin notifications-delete --all --yes"},
	},

	// ---------------------------------------------------------------- toolbox
	"toolbox pdf-split": {
		Desc:     "Splits a pdf in one pdf per page (zip).",
		Flags:    fl("<pdf> | the pdf (or --file)", "--file PATH | the pdf", outOpts),
		Examples: []string{"artcoded toolbox pdf-split scan.pdf --out pages.zip"},
	},
	"toolbox pdf-rotate": {
		Desc:     "Rotates an uploaded pdf (in place).",
		Flags:    fl("<upload id> | the pdf (or --id)", idFlag, "--rotation DEGREES | 90, 180 (default) or 270"),
		Examples: []string{"artcoded toolbox pdf-rotate 6705 --rotation 90"},
	},
	"toolbox rdf-convert": {
		Desc:     "Converts an rdf file to another syntax.",
		Flags:    fl("<file> | the rdf file (or --file)", "--file PATH | the rdf file", "--lang LANG | target syntax (see toolbox rdf-langs), e.g. TURTLE, JSONLD", outOpts),
		Examples: []string{"artcoded toolbox rdf-convert data.rdf --lang TURTLE --stdout"},
	},
	"toolbox rdf-langs": {
		Desc:     "The rdf syntaxes of rdf-convert.",
		Examples: []string{"artcoded toolbox rdf-langs"},
	},
	"toolbox shacl": {
		Desc:     "Validates an rdf model with shacl shapes; prints the report (nothing: valid).",
		Flags:    fl("<model> | the model (or --model)", "<shapes> | the shapes (or --shapes)", "--model PATH | the model", "--shapes PATH | the shapes"),
		Examples: []string{"artcoded toolbox shacl data.ttl shapes.ttl"},
	},
	"toolbox xpath": {
		Desc:     "Evaluates an xpath expression on an xml file.",
		Flags:    fl("<expression> | the expression (or --expression)", "<xml file> | the file (or --file)", "--expression XPATH | the expression", "--file PATH | the xml file"),
		Examples: []string{"artcoded toolbox xpath \"//*[local-name()='PayableAmount']\" invoice.xml"},
	},
}
