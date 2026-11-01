import { plural, type Activity } from "./api";
import { Flame } from "./icons";

// What the learner finished, day by day: a streak to keep, a year-style
// calendar, and in December an advent calendar with stars for days 1-24.
// Everything counts finished tasks and lessons, never time spent.

const WEEKS = 26;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

const pad = (n: number) => String(n).padStart(2, "0");
const dateKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);

function describe(date: Date, tasks: number, lessons: number) {
  const day = `${MONTHS[date.getMonth()]} ${date.getDate()}`;
  if (!tasks && !lessons) return `${day}: nothing finished`;
  return `${day}: ${[tasks && plural(tasks, "exercise"), lessons && plural(lessons, "lesson")].filter(Boolean).join(", ")}`;
}

export function ActivityPanel({ activity, now = new Date() }: { activity: Activity; now?: Date }) {
  const counts = new Map((activity.days ?? []).map((d) => [d.date, d]));
  const advent = now.getMonth() === 11;
  const { streak, best, today, total } = activity;

  return (
    <section className="activity" aria-label="Activity">
      <div className="activity-stats">
        <div className={`stat streak ${today > 0 ? "is-lit" : ""}`} title={today > 0 ? "Done for today" : "Not done today yet"}>
          <span className="stat-value">
            <Flame size={22} />
            {streak}
          </span>
          <span className="stat-label">day streak</span>
        </div>
        <Stat value={total} label="finished" />
        <Stat value={today} label="today" />
        <p className="activity-note">
          {total === 0 ? "Finish an exercise or lesson to start a streak." : streak > 0 && today === 0 ? "Finish an exercise or lesson today to keep your streak." : best > streak ? `Best: ${plural(best, "day")}` : streak > 1 ? "Your best streak yet." : ""}
        </p>
      </div>
      {advent ? <Advent counts={counts} now={now} /> : <Calendar counts={counts} now={now} />}
    </section>
  );
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className="stat">
      <span className="stat-value">{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  );
}

type Counts = Map<string, { tasks: number; lessons: number }>;

/** The last half year, one square per day, Monday at the top. Darker means less finished. */
function Calendar({ counts, now }: { counts: Counts; now: Date }) {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const monday = addDays(today, -((today.getDay() + 6) % 7));
  const start = addDays(monday, -7 * (WEEKS - 1));
  const weeks = Array.from({ length: WEEKS }, (_, w) => Array.from({ length: 7 }, (_, d) => addDays(start, w * 7 + d)));

  return (
    <div className="calendar" role="img" aria-label="Days with finished tasks and lessons over the last six months">
      <div className="calendar-months" aria-hidden="true">
        {weeks.map((days, w) => {
          // Label a column when a month starts in it.
          const first = days.find((d) => d.getDate() === 1);
          return <span key={w}>{first && w < WEEKS - 1 ? MONTHS[first.getMonth()] : ""}</span>;
        })}
      </div>
      <div className="calendar-grid">
        {weeks.map((days, w) => (
          <div key={w} className="calendar-week">
            {days.map((d) => {
              const key = dateKey(d);
              const c = counts.get(key);
              const n = (c?.tasks ?? 0) + (c?.lessons ?? 0);
              const level = n === 0 ? 0 : n === 1 ? 1 : n === 2 ? 2 : n <= 4 ? 3 : 4;
              const future = d > today;
              return (
                <span
                  key={key}
                  className={`cell lv${level} ${key === dateKey(today) ? "is-today" : ""} ${future ? "is-future" : ""}`}
                  title={future ? undefined : describe(d, c?.tasks ?? 0, c?.lessons ?? 0)}
                />
              );
            })}
          </div>
        ))}
      </div>
      <div className="calendar-legend" aria-hidden="true">
        <span>Less</span>
        {[0, 1, 2, 3, 4].map((l) => (
          <span key={l} className={`cell lv${l}`} />
        ))}
        <span>More</span>
      </div>
    </div>
  );
}

/**
 * December 1-24: one star for finishing something that day, a second for
 * finishing two or more. Today is bright, days ahead wait in grey.
 */
function Advent({ counts, now }: { counts: Counts; now: Date }) {
  const year = now.getFullYear();
  const todayN = now.getDate();
  const days = Array.from({ length: 24 }, (_, i) => {
    const date = new Date(year, 11, i + 1);
    const c = counts.get(dateKey(date));
    const n = (c?.tasks ?? 0) + (c?.lessons ?? 0);
    return { day: i + 1, date, c, stars: Math.min(n, 2) };
  });
  const stars = days.reduce((s, d) => s + d.stars, 0);

  return (
    <div className="advent">
      <div className="advent-head">
        <h2>Advent {year}</h2>
        <span className="num">
          {stars} ★ of 48
        </span>
      </div>
      <ol className="advent-grid">
        {days.map(({ day, date, c, stars }) => {
          const state = day === todayN ? "is-today" : day > todayN ? "is-ahead" : stars ? "is-done" : "is-missed";
          return (
            <li key={day} className={`advent-day ${state}`} title={day > todayN ? undefined : describe(date, c?.tasks ?? 0, c?.lessons ?? 0)}>
              <span className="advent-n">{day}</span>
              <span className="advent-stars" aria-label={plural(stars, "star")}>
                <span className={stars >= 1 ? "is-on" : ""}>★</span>
                <span className={stars >= 2 ? "is-on" : ""}>★</span>
              </span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
