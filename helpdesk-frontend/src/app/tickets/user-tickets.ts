import { CommonModule } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { ActivatedRoute, Router, RouterLink, RouterLinkActive } from '@angular/router';
import { finalize } from 'rxjs';
import { AuthService, HelpdeskUser } from '../auth.service';
import { MockTicketService, TicketPriority, TicketStatus, toHelpdeskTicket } from './mock-ticket.service';

@Component({
  selector: 'app-user-tickets',
  imports: [CommonModule, RouterLink, RouterLinkActive],
  templateUrl: './user-tickets.html',
  styleUrl: './all-tickets.css',
})
export class UserTickets implements OnInit {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly ticketData = inject(MockTicketService);

  protected readonly assignedView = this.route.snapshot.data['ticketView'] === 'assigned';
  protected readonly user = signal<HelpdeskUser | null>(null);
  protected readonly mobileMenuOpen = signal(false);
  protected readonly notificationsOpen = signal(false);
  protected readonly loggingOut = signal(false);
  protected readonly searchTerm = signal('');
  protected readonly priorityFilter = signal<TicketPriority | ''>('');
  protected readonly statusFilter = signal<TicketStatus | ''>('');
  protected readonly currentPage = signal(1);
  protected readonly pageSize = 8;
  protected readonly darkMode = signal(this.readDarkMode());
  protected readonly ticketLoadError = signal('');
  protected readonly ticketCount = computed(() => this.ticketData.tickets().length);

  protected readonly userTickets = computed(() => {
    const user = this.user();
    if (!user) return [];
    return this.ticketData.tickets().filter((ticket) =>
      this.assignedView
        ? ticket.assignedToId === user.id
        : ticket.createdById === user.id,
    );
  });
  protected readonly filteredTickets = computed(() => {
    const query = this.searchTerm().trim().toLowerCase();
    return this.userTickets().filter((ticket) =>
      (!query || ticket.number.toLowerCase().includes(query) || ticket.title.toLowerCase().includes(query)) &&
      (!this.priorityFilter() || ticket.priority === this.priorityFilter()) &&
      (!this.statusFilter() || ticket.status === this.statusFilter()),
    );
  });
  protected readonly totalPages = computed(() => Math.max(1, Math.ceil(this.filteredTickets().length / this.pageSize)));
  protected readonly visibleTickets = computed(() => {
    const start = (this.currentPage() - 1) * this.pageSize;
    return this.filteredTickets().slice(start, start + this.pageSize);
  });
  protected readonly firstResult = computed(() => this.filteredTickets().length ? (this.currentPage() - 1) * this.pageSize + 1 : 0);
  protected readonly lastResult = computed(() => Math.min(this.currentPage() * this.pageSize, this.filteredTickets().length));
  protected readonly myTicketCount = computed(() => {
    const user = this.user();
    return user ? this.ticketData.tickets().filter((ticket) => ticket.createdById === user.id).length : 0;
  });
  protected readonly assignedTicketCount = computed(() => {
    const user = this.user();
    return user ? this.ticketData.tickets().filter((ticket) => ticket.assignedToId === user.id).length : 0;
  });

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

  protected updateSearch(value: string): void { this.searchTerm.set(value); this.currentPage.set(1); }
  protected updatePriority(value: string): void { this.priorityFilter.set(value as TicketPriority | ''); this.currentPage.set(1); }
  protected updateStatus(value: string): void { this.statusFilter.set(value as TicketStatus | ''); this.currentPage.set(1); }
  protected goToPage(page: number): void { this.currentPage.set(Math.min(Math.max(page, 1), this.totalPages())); }
  protected pageNumbers(): number[] { return Array.from({ length: this.totalPages() }, (_, index) => index + 1); }
  protected closeMobileMenu(): void { this.mobileMenuOpen.set(false); }

  protected toggleTheme(): void {
    this.darkMode.update((enabled) => !enabled);
    try { window.localStorage.setItem('helpdesk_dark_mode', String(this.darkMode())); } catch { /* retain current page theme */ }
    this.applyTheme();
  }

  protected logout(): void {
    if (this.loggingOut()) return;
    this.loggingOut.set(true);
    this.auth.logout().pipe(finalize(() => this.loggingOut.set(false))).subscribe({
      next: () => void this.router.navigateByUrl('/login'),
      error: () => void this.router.navigateByUrl('/login'),
    });
  }

  private readDarkMode(): boolean {
    try { return window.localStorage.getItem('helpdesk_dark_mode') === 'true'; } catch { return false; }
  }

  private applyTheme(): void { document.documentElement.dataset['theme'] = this.darkMode() ? 'dark' : 'light'; }
}
