import type { Job, CreateJobRequest, ApiError, Place, Tweet } from "@/types"
import { supabase, isSupabaseConfigured } from "@/lib/supabase"

const API_BASE = "/api/v1"

async function getAuthHeaders(): Promise<Record<string, string>> {
  const headers: Record<string, string> = {}
  if (isSupabaseConfigured) {
    const { data: { session } } = await supabase.auth.getSession()
    if (session?.access_token) {
      headers["Authorization"] = `Bearer ${session.access_token}`
    }
  }
  return headers
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const error: ApiError = await response.json()
    throw new Error(error.message || `HTTP ${response.status}`)
  }
  return response.json()
}

function normalizeJob(job: any): Job {
  return {
    id: job?.id || job?.ID || "",
    name: job?.name || job?.Name || "",
    date: job?.date || job?.Date || "",
    status: (job?.status || job?.Status || "pending").toLowerCase(),
    type: job?.type || job?.Type,
    data: job?.data || job?.Data || {},
  }
}

export async function getJobs(type?: string): Promise<Job[]> {
  const authHeaders = await getAuthHeaders()
  const url = type ? `${API_BASE}/jobs?type=${type}` : `${API_BASE}/jobs`
  const response = await fetch(url, {
    headers: authHeaders,
  })
  const data = await handleResponse<any[]>(response)
  return (data || []).map(normalizeJob)
}

export async function getJob(id: string): Promise<Job> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}`, {
    headers: authHeaders,
  })
  const data = await handleResponse<any>(response)
  return normalizeJob(data)
}

export async function createJob(job: CreateJobRequest): Promise<{ id: string }> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...authHeaders,
    },
    body: JSON.stringify(job),
  })
  return handleResponse<{ id: string }>(response)
}

export async function deleteJob(id: string): Promise<void> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}`, {
    method: "DELETE",
    headers: authHeaders,
  })
  if (!response.ok) {
    const error: ApiError = await response.json()
    throw new Error(error.message || `HTTP ${response.status}`)
  }
}

export async function getJobPlaces(id: string): Promise<Place[]> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}/places`, {
    headers: authHeaders,
  })
  return handleResponse<Place[]>(response)
}

export async function getJobTweets(id: string): Promise<Tweet[]> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}/tweets`, {
    headers: authHeaders,
  })
  return handleResponse<Tweet[]>(response)
}

export async function getJobCSV(id: string): Promise<string> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}/download`, {
    headers: authHeaders,
  })
  if (!response.ok) {
    throw new Error("Failed to load places")
  }
  return response.text()
}

export async function downloadJobFile(id: string, name: string): Promise<void> {
  const authHeaders = await getAuthHeaders()
  const response = await fetch(`${API_BASE}/jobs/${id}/download`, {
    headers: authHeaders,
  })
  
  if (!response.ok) {
    const error: ApiError = await response.json()
    throw new Error(error.message || `HTTP ${response.status}`)
  }

  const blob = await response.blob()
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = name || "download.csv"
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}
