import { CommonModule } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { Router, RouterLink, RouterLinkActive } from '@angular/router';
import { finalize } from 'rxjs';
import { AuthService, HelpdeskUser } from '../auth.service';
import { HelpdeskTicket, MockTicketService, toHelpdeskTicket } from '../tickets/mock-ticket.service';

interface TicketRow {
  number: string;
  title: string;
  category: string;
  priority: 'Critical' | 'High' | 'Medium' | 'Low';
  status: 'Open' | 'Assigned' | 'In Progress' | 'Resolved' | 'Pending Admin Approval' | 'Pending Admin Resolution Review' | 'Pending User Approval' | 'Closed';
  assignedTo: string;
  createdAt: string;
  initials: string;
  avatar: string;
}

@Component({
  selector: 'app-dashboard',
  imports: [CommonModule, RouterLink, RouterLinkActive],
  templateUrl: './dashboard.html',
})
export class Dashboard implements OnInit {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly ticketData = inject(MockTicketService);

  protected readonly user = signal<HelpdeskUser | null>(null);
  protected readonly searchTerm = signal('');
  protected readonly notificationsOpen = signal(false);
  protected readonly mobileMenuOpen = signal(false);
  protected readonly loggingOut = signal(false);
  protected readonly darkMode = signal(this.readDarkMode());
  protected readonly ticketLoadError = signal('');

  protected readonly allTickets = this.ticketData.tickets;
  protected readonly tickets = computed(() => this.allTickets().map((ticket) => this.toDashboardRow(ticket)));
  protected readonly stats = computed(() => {
    const tickets = this.allTickets();
    return {
      total: tickets.length,
      open: tickets.filter((ticket) => ticket.status === 'OPEN').length,
      inProgress: tickets.filter((ticket) => ticket.status === 'IN_PROGRESS').length,
      resolved: tickets.filter((ticket) => ticket.status === 'RESOLVED').length,
      critical: tickets.filter((ticket) => ticket.priority === 'CRITICAL').length,
      assigned: tickets.filter((ticket) => ticket.status === 'ASSIGNED').length,
      closed: tickets.filter((ticket) => ticket.status === 'CLOSED').length,
      pendingApproval: tickets.filter((ticket) => ticket.status === 'PENDING_APPROVAL' || ticket.status === 'PENDING_RESOLUTION_APPROVAL' || ticket.status === 'PENDING_USER_CONFIRMATION').length,
      low: tickets.filter((ticket) => ticket.priority === 'LOW').length,
      medium: tickets.filter((ticket) => ticket.priority === 'MEDIUM').length,
      high: tickets.filter((ticket) => ticket.priority === 'HIGH').length,
    };
  });

  protected readonly filteredTickets = computed(() => {
    const query = this.searchTerm().trim().toLowerCase();
    if (!query) return this.tickets().slice(0, 5);
    return this.tickets().filter((ticket) =>
      [ticket.number, ticket.title, ticket.category, ticket.priority, ticket.status, ticket.assignedTo]
        .some((value) => value.toLowerCase().includes(query)),
    ).slice(0, 5);
  });

  protected donutDash(value: number): string {
    const total = this.stats().total || 1;
    return `${(value / total) * 326.7} 326.7`;
  }

  protected donutOffset(...previousValues: number[]): string {
    const total = this.stats().total || 1;
    return `${-(previousValues.reduce((sum, value) => sum + value, 0) / total) * 326.7}`;
  }

  protected priorityWidth(value: number): string {
    const maximum = Math.max(this.stats().low, this.stats().medium, this.stats().high, this.stats().critical, 1);
    return `${(value / maximum) * 100}%`;
  }

  private toDashboardRow(ticket: HelpdeskTicket): TicketRow {
    return {
      ...ticket,
      priority: ticket.priority.charAt(0) + ticket.priority.slice(1).toLowerCase() as TicketRow['priority'],
      status: ticket.status === 'PENDING_APPROVAL' ? 'Pending Admin Approval' : ticket.status === 'PENDING_RESOLUTION_APPROVAL' ? 'Pending Admin Resolution Review' : ticket.status === 'PENDING_USER_CONFIRMATION' ? 'Pending User Approval' : ticket.status.replace('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) as TicketRow['status'],
    };
  }

  ngOnInit(): void {
    this.applyTheme();
    this.auth.tickets().subscribe({
      next: ({ tickets }) => {
        this.ticketData.replaceTickets(tickets.map(toHelpdeskTicket));
        this.ticketLoadError.set('');
      },
      error: () => this.ticketLoadError.set('Could not load live tickets. Check that the backend is running and try refreshing.'),
    });
    this.auth.currentUser().subscribe({
      next: ({ user }) => this.user.set(user),
      error: () => {
        this.auth.clearStoredSession();
        void this.router.navigateByUrl('/login');
      },
    });
  }

  protected toggleTheme(): void {
    this.darkMode.update((enabled) => !enabled);
    try {
      window.localStorage.setItem('helpdesk_dark_mode', String(this.darkMode()));
    } catch {
      // Keep the current theme for this page even if storage is unavailable.
    }
    this.applyTheme();
  }

  private readDarkMode(): boolean {
    try {
      return window.localStorage.getItem('helpdesk_dark_mode') === 'true';
    } catch {
      return false;
    }
  }

  private applyTheme(): void {
    document.documentElement.dataset['theme'] = this.darkMode() ? 'dark' : 'light';
  }

  protected logout(): void {
    if (this.loggingOut()) return;
    this.loggingOut.set(true);
    this.auth.logout().pipe(finalize(() => this.loggingOut.set(false))).subscribe({
      next: () => void this.router.navigateByUrl('/login'),
      error: () => void this.router.navigateByUrl('/login'),
    });
  }

  protected closeMobileMenu(): void {
    this.mobileMenuOpen.set(false);
  }
}
