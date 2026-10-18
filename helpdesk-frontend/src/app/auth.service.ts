import { HttpClient, HttpHeaders } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, TimeoutError, catchError, concatMap, finalize, map, of, throwError, timeout } from 'rxjs';

export interface HelpdeskUser {
  id: number;
  full_name: string;
  email: string;
  role: 'USER' | 'SUPPORT' | 'ADMIN';
}

export interface LoginResponse {
  message: string;
  access_token: string;
  token_type: 'Bearer';
  expires_in: number;
  user: HelpdeskUser;
}

export interface CurrentUserResponse {
  user: HelpdeskUser;
}

export interface HelpdeskCategory { id: number; name: string; description: string; }
export interface CategoriesResponse { categories: HelpdeskCategory[]; }
export interface CreateTicketResponse {
  id: number;
  ticket_number: string;
  title: string;
  description: string;
  category_id: number;
  priority: 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
  status: 'OPEN' | 'ASSIGNED' | 'IN_PROGRESS' | 'RESOLVED' | 'PENDING_APPROVAL' | 'PENDING_RESOLUTION_APPROVAL' | 'PENDING_USER_CONFIRMATION' | 'CLOSED';
  assigned_to: number | null;
  created_by: number;
  creator: HelpdeskUser;
  created_at: string;
  updated_at: string;
}
export interface TicketApiItem {
  id: number;
  ticket_number: string;
  title: string;
  description: string;
  category_id: number;
  category_name: string;
  priority: 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
  status: 'OPEN' | 'ASSIGNED' | 'IN_PROGRESS' | 'RESOLVED' | 'PENDING_APPROVAL' | 'PENDING_RESOLUTION_APPROVAL' | 'PENDING_USER_CONFIRMATION' | 'CLOSED';
  assigned_to: number | null;
  assigned_to_name: string;
  created_by: number;
  created_by_name: string;
  resolution_description: string | null;
  created_at: string;
  updated_at: string;
}
export interface TicketsResponse { tickets: TicketApiItem[]; }
export interface SupportUsersResponse { users: HelpdeskUser[]; }
export interface TicketComment {
  id: number;
  ticket_id: number;
  user_id: number;
  user_name: string;
  comment_text: string;
  created_at: string;
}

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly http = inject(HttpClient);
  private readonly authUrl = 'http://localhost:8080/api/auth';
  private readonly apiUrl = 'http://localhost:8080/api';

  login(email: string, password: string, rememberMe: boolean): Observable<LoginResponse> {
    this.clearSession();
    return this.http
      .post<LoginResponse>(`${this.authUrl}/login`, {
        email,
        password,
        remember_me: rememberMe,
      })
      .pipe(timeout({ first: 15000 }));
  }

  saveSession(response: LoginResponse, rememberMe: boolean): boolean {
    this.clearSession();
    const storage = this.getStorage(rememberMe ? 'local' : 'session');
    if (!storage) return false;

    try {
      storage.setItem('helpdesk_access_token', response.access_token);
      return true;
    } catch {
      return false;
    }
  }

  currentUser(): Observable<CurrentUserResponse> {
    const token = this.getToken();
    return this.http.get<CurrentUserResponse>(`${this.authUrl}/me`, { headers: this.authHeaders(token) });
  }

  categories(): Observable<CategoriesResponse> {
    return this.http.get<CategoriesResponse>(`${this.apiUrl}/categories`, { headers: this.authHeaders(this.getToken()) });
  }

  createTicket(request: { title: string; description: string; category_id: number; priority: string }): Observable<CreateTicketResponse> {
    return this.http.post<CreateTicketResponse>(`${this.apiUrl}/tickets`, request, { headers: this.authHeaders(this.getToken()) });
  }

  tickets(): Observable<TicketsResponse> {
    return this.http.get<TicketsResponse>(`${this.apiUrl}/tickets`, { headers: this.authHeaders(this.getToken()) });
  }

  ticket(ticketNumber: string): Observable<TicketApiItem> {
    return this.http.get<TicketApiItem>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}`, { headers: this.authHeaders(this.getToken()) });
  }

  supportUsers(): Observable<SupportUsersResponse> {
    return this.http.get<SupportUsersResponse>(`${this.apiUrl}/support-users`, { headers: this.authHeaders(this.getToken()) });
  }

  updateTicketStatus(ticketNumber: string, status: string, resolutionDescription: string): Observable<{ status: string; resolution_description: string }> {
    return this.http.patch<{ status: string; resolution_description: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/status`, { status, resolution_description: resolutionDescription }, { headers: this.authHeaders(this.getToken()) });
  }

  reviewTicketClosure(ticketNumber: string, action: 'approve' | 'reject', reason: string): Observable<{ status: string }> {
    return this.http.patch<{ status: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/closure`, { action, reason }, { headers: this.authHeaders(this.getToken()) });
  }

  reviewTicketResolution(ticketNumber: string, action: 'approve' | 'reject', reason: string): Observable<{ status: string }> {
    return this.http.patch<{ status: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/resolution-review`, { action, reason }, { headers: this.authHeaders(this.getToken()) });
  }

  confirmTicketResolution(ticketNumber: string): Observable<{ status: string }> {
    return this.http.patch<{ status: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/resolution-confirmation`, {}, { headers: this.authHeaders(this.getToken()) });
  }

  assignTicket(ticketNumber: string, assignedTo: number | null): Observable<{ assigned_to: number | null }> {
    return this.http.patch<{ assigned_to: number | null }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/assignment`, { assigned_to: assignedTo }, { headers: this.authHeaders(this.getToken()) });
  }

  updateTicket(ticketNumber: string, status: string, assignedTo: number | null, resolutionDescription: string): Observable<{ status: string; assigned_to: number | null; resolution_description: string }> {
    const headers = { headers: this.authHeaders(this.getToken()) };
    return this.http.patch<{ status: string; assigned_to: number | null; resolution_description: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}`, { status, assigned_to: assignedTo, resolution_description: resolutionDescription }, headers).pipe(
      catchError((error) => {
        // Keep compatibility with a backend process that is still running the
        // earlier version, which exposes status and assignment separately.
        if (error.status !== 404 && error.status !== 405) return throwError(() => error);
        return this.assignTicket(ticketNumber, assignedTo).pipe(
          concatMap(() => this.updateTicketStatus(ticketNumber, status, resolutionDescription)),
          map((result) => ({ status: result.status, assigned_to: assignedTo, resolution_description: result.resolution_description })),
        );
      }),
    );
  }

  updateTicketPriority(ticketNumber: string, priority: string): Observable<{ priority: string }> {
    return this.http.patch<{ priority: string }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/priority`, { priority }, { headers: this.authHeaders(this.getToken()) });
  }

  ticketComments(ticketNumber: string): Observable<{ comments: TicketComment[] }> {
    return this.http.get<{ comments: TicketComment[] }>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/comments`, { headers: this.authHeaders(this.getToken()) });
  }

  addTicketComment(ticketNumber: string, commentText: string): Observable<TicketComment> {
    return this.http.post<TicketComment>(`${this.apiUrl}/tickets/${encodeURIComponent(ticketNumber)}/comments`, { comment_text: commentText }, { headers: this.authHeaders(this.getToken()) });
  }

  logout(): Observable<{ message: string }> {
    const token = this.getToken();
    if (!token) return of({ message: 'Logged out successfully' });

    return this.http
      .post<{ message: string }>(
        `${this.authUrl}/logout`,
        {},
        { headers: this.authHeaders(token) },
      )
      .pipe(finalize(() => this.clearSession()));
  }

  hasSession(): boolean {
    return this.getToken() !== '';
  }

  clearStoredSession(): void {
    this.clearSession();
  }

  private getToken(): string {
    return this.readToken('session') ?? this.readToken('local') ?? '';
  }

  private authHeaders(token: string): HttpHeaders {
    return new HttpHeaders({ Authorization: `Bearer ${token}` });
  }

  private clearSession(): void {
    for (const kind of ['session', 'local'] as const) {
      try {
        this.getStorage(kind)?.removeItem('helpdesk_access_token');
      } catch {
        // Storage can be blocked by the browser; it must not block the API request.
      }
    }
  }

  private readToken(kind: 'local' | 'session'): string | null {
    try {
      return this.getStorage(kind)?.getItem('helpdesk_access_token') ?? null;
    } catch {
      return null;
    }
  }

  private getStorage(kind: 'local' | 'session'): Storage | null {
    try {
      return kind === 'local' ? window.localStorage : window.sessionStorage;
    } catch {
      return null;
    }
  }
}
