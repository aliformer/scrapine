export interface Job {
  id: string
  name: string
  date: string
  status: "pending" | "working" | "ok" | "failed"
  type?: "gmaps" | "twitter" | "threads" | "facebook" | "tiktok"
  data: JobData
}

export interface JobData {
  keywords: string[]
  lang: string
  zoom: number
  lat: string
  lon: string
  fast_mode: boolean
  radius: number
  depth: number
  max_results?: number
  email: boolean
  extra_reviews: boolean
  max_time: number
  proxies: string[]
  cookie?: string
  username?: string
  query?: string
}

export interface CreateJobRequest {
  name: string
  keywords: string[]
  lang: string
  zoom: number
  lat: string
  lon: string
  fast_mode: boolean
  radius: number
  depth: number
  max_results?: number
  email: boolean
  extra_reviews: boolean
  max_time: number
  proxies: string[]
  cookie?: string
  type?: "gmaps" | "twitter" | "threads" | "facebook" | "tiktok"
  username?: string
  query?: string
}

export interface Place {
  title: string
  address: string
  latitude: number
  longitude: number
  rating?: number
  reviews?: number
  phone?: string
  website?: string
  category?: string
}

export interface Tweet {
  id: string
  url: string
  author_username: string
  author_name: string
  text: string
  created_at: string
  replies: number
  retweets: number
  likes: number
  views: number
  is_reply: boolean
  is_retweet: boolean
}

export interface ApiError {
  code: number
  message: string
}
