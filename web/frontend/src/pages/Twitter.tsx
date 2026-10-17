import { useState, useEffect } from "react"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { JobTable } from "@/components/JobTable"
import { CookieAuth } from "@/components/CookieAuth"
import { TwitterFeedModal } from "@/components/TwitterFeedModal"
import { getJobs, createJob, getJobTweets } from "@/api/client"
import type { Job, CreateJobRequest, Tweet } from "@/types"
import { Loader2, Twitter } from "lucide-react"
import { useAuth } from "@/context/AuthContext"

export function TwitterPage() {
  const { user, signInWithTwitter } = useAuth()
  const [jobs, setJobs] = useState<Job[]>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [connectingTwitter, setConnectingTwitter] = useState(false)

  const [jobName, setJobName] = useState("")
  const [username, setUsername] = useState("")
  const [query, setQuery] = useState("")
  const [cookie, setCookie] = useState("")
  const [depth, setDepth] = useState(1)
  const [maxResults, setMaxResults] = useState(100)

  const [selectedJob, setSelectedJob] = useState<{ id: string; name: string } | null>(null)
  const [tweets, setTweets] = useState<Tweet[]>([])
  const [feedLoading, setFeedLoading] = useState(false)
  const [feedError, setFeedError] = useState<string | null>(null)

  const handleViewJob = async (job: Job) => {
    setSelectedJob({ id: job.id, name: job.name || job.id })
    setFeedLoading(true)
    setFeedError(null)
    try {
      const data = await getJobTweets(job.id)
      setTweets(data)
    } catch (err) {
      setFeedError(err instanceof Error ? err.message : "Failed to load tweets")
    } finally {
      setFeedLoading(false)
    }
  }

  const handleConnectTwitter = async () => {
    setConnectingTwitter(true)
    const { error } = await signInWithTwitter()
    if (error) {
      setError(error.message)
    }
    setConnectingTwitter(false)
  }

  const fetchJobs = async () => {
    try {
      const data = await getJobs("twitter")
      setJobs(data)
    } catch (err) {
      console.error("Failed to fetch Twitter jobs:", err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchJobs()
    const interval = setInterval(fetchJobs, 10000)
    return () => clearInterval(interval)
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!jobName.trim()) return

    setError(null)
    setSubmitting(true)

    const payload: CreateJobRequest = {
      name: jobName,
      type: "twitter",
      username: username.trim(),
      query: query.trim(),
      cookie: cookie.trim(),
      depth,
      max_results: maxResults,
      keywords: [],
      lang: "en",
      zoom: 1,
      lat: "",
      lon: "",
      fast_mode: false,
      radius: 0,
      email: false,
      extra_reviews: false,
      max_time: 0,
      proxies: [],
    }

    try {
      await createJob(payload)
      setJobName("")
      setUsername("")
      setQuery("")
      setCookie("")
      await fetchJobs()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create Twitter job")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold tracking-tight flex items-center space-x-2">
            <Twitter className="w-8 h-8 text-sky-500" />
            <span>X (Twitter) Scraper</span>
          </h1>
          <p className="text-muted-foreground">Scrape user tweets, profiles, and hashtag feeds.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={handleConnectTwitter}
            disabled={connectingTwitter}
            variant={user?.app_metadata?.provider === "twitter" ? "outline" : "default"}
          >
            {connectingTwitter ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Twitter className="w-4 h-4 mr-2" />
            )}
            {user?.app_metadata?.provider === "twitter" ? "Connected" : "Connect X"}
          </Button>
          <CookieAuth platform="twitter" />
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>New X (Twitter) Scrape Job</CardTitle>
          <CardDescription>Target a user handle or a search query.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && <div className="p-3 text-sm bg-destructive/10 text-destructive rounded-md">{error}</div>}

            <div className="grid md:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="name">Job Name</Label>
                <Input
                  id="name"
                  placeholder="e.g., Tech Influencers Feed"
                  value={jobName}
                  onChange={(e) => setJobName(e.target.value)}
                  required
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="username">Username (Optional)</Label>
                <Input
                  id="username"
                  placeholder="e.g., elonmusk (comma- or newline-separated run in parallel)"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>
            </div>

            <div className="grid md:grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label htmlFor="query">Search Query / Hashtag (Optional)</Label>
                <Input
                  id="query"
                  placeholder="e.g., #golang, #ai (comma- or newline-separated run in parallel)"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="depth">Max Scroll Depth / Pages</Label>
                <Input
                  id="depth"
                  type="number"
                  min={1}
                  max={50}
                  value={depth}
                  onChange={(e) => setDepth(parseInt(e.target.value) || 1)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="maxResults">Target Tweet Count</Label>
                <Input
                  id="maxResults"
                  type="number"
                  min={1}
                  max={1000}
                  value={maxResults}
                  onChange={(e) => setMaxResults(parseInt(e.target.value) || 100)}
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="cookie">Auth Cookies / Tokens (Optional - one per line for multiple accounts, or leave empty for cookie pool)</Label>
              <Textarea
                id="cookie"
                placeholder={"auth_token=acc1_token; ct0=acc1_csrf\nauth_token=acc2_token; ct0=acc2_csrf"}
                value={cookie}
                onChange={(e) => setCookie(e.target.value)}
                rows={2}
                className="font-mono text-xs"
              />
            </div>

            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Start X Job
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>X Scraping Jobs</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex justify-center items-center py-8">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <JobTable jobs={jobs} onJobDeleted={fetchJobs} onViewJob={handleViewJob} />
          )}
        </CardContent>
      </Card>

      {selectedJob && (
        <TwitterFeedModal
          job={selectedJob}
          tweets={tweets}
          loading={feedLoading}
          error={feedError}
          onClose={() => setSelectedJob(null)}
        />
      )}
    </div>
  )
}
