import { useState, useMemo } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import type { Tweet } from "@/types"
import {
  MessageCircle,
  Repeat2,
  Heart,
  BarChart2,
  ExternalLink,
  Loader2,
  Search,
  CheckCircle2,
  Twitter,
} from "lucide-react"

interface TwitterFeedModalProps {
  job: { id: string; name: string } | null
  tweets: Tweet[]
  loading: boolean
  error: string | null
  onClose: () => void
}

function formatCount(num: number): string {
  if (!num) return "0"
  if (num >= 1_000_000) return (num / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M"
  if (num >= 1_000) return (num / 1_000).toFixed(1).replace(/\.0$/, "") + "K"
  return num.toLocaleString()
}

function formatDate(dateStr: string): string {
  if (!dateStr) return ""
  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return dateStr
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: date.getFullYear() !== new Date().getFullYear() ? "numeric" : undefined,
  })
}

function getInitials(name: string, username: string): string {
  const target = name || username || "X"
  return target.slice(0, 2).toUpperCase()
}

export function TwitterFeedModal({ job, tweets = [], loading, error, onClose }: TwitterFeedModalProps) {
  const [search, setSearch] = useState("")

  const filteredTweets = useMemo(() => {
    if (!search.trim()) return tweets
    const term = search.toLowerCase()
    return tweets.filter((tw) =>
      tw.text.toLowerCase().includes(term) ||
      tw.author_name.toLowerCase().includes(term) ||
      tw.author_username.toLowerCase().includes(term)
    )
  }, [tweets, search])

  if (!job) return null

  return (
    <Dialog open={!!job} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-4xl h-[90vh] flex flex-col p-0 overflow-hidden bg-background">
        <DialogHeader className="p-4 border-b shrink-0 flex flex-row items-center justify-between space-y-0 pr-10">
          <div>
            <DialogTitle className="flex items-center gap-2 text-xl font-bold">
              <Twitter className="w-5 h-5 text-sky-500 fill-sky-500" />
              <span>{job.name}</span>
              <Badge variant="secondary" className="ml-2">
                {tweets.length} {tweets.length === 1 ? "tweet" : "tweets"}
              </Badge>
            </DialogTitle>
            <DialogDescription className="text-xs text-muted-foreground mt-0.5">
              Original X (Twitter) feed view
            </DialogDescription>
          </div>
        </DialogHeader>

        <div className="px-4 py-2 border-b bg-muted/30 shrink-0">
          <div className="relative">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search in tweets, authors, or @handles..."
              className="pl-9 h-9 text-sm bg-background"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
        </div>

        <div className="flex-1 min-h-0">
          {loading && (
            <div className="h-full flex flex-col items-center justify-center gap-3 text-muted-foreground">
              <Loader2 className="h-8 w-8 animate-spin text-sky-500" />
              <p className="text-sm">Loading tweets...</p>
            </div>
          )}

          {error && (
            <div className="h-full flex items-center justify-center text-destructive p-4 text-center">
              <p className="text-sm">{error}</p>
            </div>
          )}

          {!loading && !error && filteredTweets.length === 0 && (
            <div className="h-full flex flex-col items-center justify-center text-muted-foreground p-8 text-center">
              <Twitter className="w-12 h-12 text-muted-foreground/30 mb-2" />
              <p className="font-medium text-foreground">No tweets found</p>
              <p className="text-xs text-muted-foreground mt-1">
                {tweets.length === 0
                  ? "This scrape job hasn't captured any tweets yet."
                  : "No tweets matched your search query."}
              </p>
            </div>
          )}

          {!loading && !error && filteredTweets.length > 0 && (
            <ScrollArea className="h-full">
              <div className="divide-y divide-border">
                {filteredTweets.map((tw) => {
                  const authorName = tw.author_name || tw.author_username || "User"
                  const handle = tw.author_username ? `@${tw.author_username}` : ""
                  const tweetUrl = tw.url || (tw.id ? `https://x.com/i/status/${tw.id}` : "#")

                  return (
                    <article
                      key={tw.id || Math.random()}
                      className="p-4 hover:bg-muted/40 transition-colors flex gap-3 text-foreground"
                    >
                      {/* Avatar */}
                      <div className="shrink-0">
                        <div className="w-10 h-10 rounded-full bg-muted border flex items-center justify-center text-xs font-bold text-muted-foreground select-none">
                          {getInitials(tw.author_name, tw.author_username)}
                        </div>
                      </div>

                      {/* Content */}
                      <div className="flex-1 min-w-0 space-y-1.5">
                        {/* Header: Name, Handle, Date, Link */}
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-1.5 flex-wrap min-w-0">
                            <span className="font-bold text-sm truncate max-w-[200px] hover:underline cursor-pointer">
                              {authorName}
                            </span>
                            <CheckCircle2 className="w-3.5 h-3.5 text-sky-500 shrink-0 fill-sky-500/20" />
                            {handle && (
                              <span className="text-xs text-muted-foreground truncate max-w-[150px]">
                                {handle}
                              </span>
                            )}
                            <span className="text-xs text-muted-foreground">·</span>
                            <span className="text-xs text-muted-foreground whitespace-nowrap">
                              {formatDate(tw.created_at)}
                            </span>
                          </div>

                          <a
                            href={tweetUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-muted shrink-0 transition-colors"
                            title="Open on X"
                          >
                            <ExternalLink className="w-3.5 h-3.5" />
                          </a>
                        </div>

                        {/* Badges for Retweet or Reply */}
                        {(tw.is_retweet || tw.is_reply) && (
                          <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            {tw.is_retweet && (
                              <span className="flex items-center gap-1 text-emerald-500 font-medium">
                                <Repeat2 className="w-3 h-3" /> Reposted
                              </span>
                            )}
                            {tw.is_reply && (
                              <span className="text-muted-foreground">Replying to post</span>
                            )}
                          </div>
                        )}

                        {/* Tweet Text */}
                        <p className="text-sm text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">
                          {tw.text}
                        </p>

                        {/* Action Bar (Metrics) */}
                        <div className="flex items-center justify-between pt-2 text-muted-foreground max-w-md text-xs select-none">
                          <div className="flex items-center gap-1.5 hover:text-sky-500 cursor-pointer transition-colors group">
                            <div className="p-1.5 rounded-full group-hover:bg-sky-500/10">
                              <MessageCircle className="w-4 h-4" />
                            </div>
                            <span>{formatCount(tw.replies)}</span>
                          </div>

                          <div className="flex items-center gap-1.5 hover:text-emerald-500 cursor-pointer transition-colors group">
                            <div className="p-1.5 rounded-full group-hover:bg-emerald-500/10">
                              <Repeat2 className="w-4 h-4" />
                            </div>
                            <span>{formatCount(tw.retweets)}</span>
                          </div>

                          <div className="flex items-center gap-1.5 hover:text-rose-500 cursor-pointer transition-colors group">
                            <div className="p-1.5 rounded-full group-hover:bg-rose-500/10">
                              <Heart className="w-4 h-4" />
                            </div>
                            <span>{formatCount(tw.likes)}</span>
                          </div>

                          <div className="flex items-center gap-1.5 hover:text-sky-500 cursor-pointer transition-colors group">
                            <div className="p-1.5 rounded-full group-hover:bg-sky-500/10">
                              <BarChart2 className="w-4 h-4" />
                            </div>
                            <span>{formatCount(tw.views)}</span>
                          </div>
                        </div>
                      </div>
                    </article>
                  )
                })}
              </div>
            </ScrollArea>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
