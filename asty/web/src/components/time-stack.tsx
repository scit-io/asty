interface TimeStackProps {
  date: Date
  // compact = single-line "time · date" for inline contexts (e.g.
  // Last-action cells, where time+date share one row). Default =
  // two-line stack used in table cells (time large, date muted).
  compact?: boolean
}

// TimeStack renders the project's two standard time+date layouts.
// Callers preprocess any epoch/string source into a Date (seconds-vs-
// millis ambiguity stays at the call site where the unit is known);
// TimeStack only owns presentation.
export function TimeStack({ date, compact }: TimeStackProps) {
  if (compact) {
    return (
      <span>
        {date.toLocaleTimeString()} · <span className="text-muted-foreground">{date.toLocaleDateString()}</span>
      </span>
    )
  }
  return (
    <div className="space-y-1">
      <div className="text-sm font-medium">{date.toLocaleTimeString()}</div>
      <div className="text-xs text-muted-foreground">{date.toLocaleDateString()}</div>
    </div>
  )
}
