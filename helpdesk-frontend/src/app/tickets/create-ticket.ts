import { CommonModule } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Router, RouterLink, RouterLinkActive } from '@angular/router';
import { HttpErrorResponse } from '@angular/common/http';
import { finalize } from 'rxjs';
import { AuthService, HelpdeskCategory, HelpdeskUser } from '../auth.service';
import { MockTicketService, TicketPriority, toHelpdeskTicket } from './mock-ticket.service';

@Component({
  selector: 'app-create-ticket',
  imports: [CommonModule, ReactiveFormsModule, RouterLink, RouterLinkActive],
  templateUrl: './create-ticket.html',
  styleUrl: './create-ticket.css',
})
export class CreateTicket implements OnInit {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly formBuilder = inject(FormBuilder);
  private readonly ticketData = inject(MockTicketService);

  protected readonly user = signal<HelpdeskUser | null>(null);
  protected readonly mobileMenuOpen = signal(false);
  protected readonly notificationsOpen = signal(false);
  protected readonly loggingOut = signal(false);
  protected readonly darkMode = signal(this.readDarkMode());
  protected readonly ticketCount = computed(() => this.ticketData.tickets().length);
  protected readonly submitted = signal(false);
  protected readonly createdTicketNumber = signal('');
  protected readonly submitError = signal('');
  protected readonly categories = signal<HelpdeskCategory[]>([]);
  protected readonly categoriesLoading = signal(false);
  protected readonly submitting = signal(false);
  protected readonly criticalPriority = signal(false);

  protected readonly form = this.formBuilder.nonNullable.group({
    title: ['', [Validators.required, Validators.pattern(/\S/), Validators.maxLength(120)]],
    description: ['', [Validators.required, Validators.pattern(/\S/), Validators.maxLength(2000)]],
    categoryId: ['', Validators.required],
    priority: ['' as TicketPriority | '', Validators.required],
  });

  ngOnInit(): void {
    this.applyTheme();
    this.auth.tickets().subscribe({ next: ({ tickets }) => this.ticketData.replaceTickets(tickets.map(toHelpdeskTicket)) });
    this.categoriesLoading.set(true);
    this.auth.categories().pipe(finalize(() => this.categoriesLoading.set(false))).subscribe({
      next: ({ categories }) => this.categories.set(categories),
      error: (error: HttpErrorResponse) => this.submitError.set(this.errorMessage(error, 'Could not load categories. Refresh the page and try again.')),
    });
    this.auth.currentUser().subscribe({
      next: ({ user }) => this.user.set(user),
      error: () => {
        this.auth.clearStoredSession();
        void this.router.navigateByUrl('/login');
      },
    });
  }

  protected prioritySelected(value: string): void {
    this.criticalPriority.set(value === 'CRITICAL');
    this.submitError.set('');
  }

  protected submitTicket(): void {
    this.submitted.set(true);
    this.submitError.set('');
    this.form.markAllAsTouched();
    if (this.form.invalid) return;

    const value = this.form.getRawValue();
    if (!value.priority || !value.categoryId) return;
    this.submitting.set(true);
    this.auth.createTicket({
      title: value.title.trim(),
      description: value.description.trim(),
      category_id: Number(value.categoryId),
      priority: value.priority,
    }).pipe(finalize(() => this.submitting.set(false))).subscribe({
      next: (ticket) => {
        this.createdTicketNumber.set(ticket.ticket_number);
        this.form.reset({ title: '', description: '', categoryId: '', priority: '' });
        this.submitted.set(false);
        this.criticalPriority.set(false);
      },
      error: (error: HttpErrorResponse) => this.submitError.set(this.errorMessage(error, 'Could not create the ticket. Please try again.')),
    });
  }

  private errorMessage(error: HttpErrorResponse, fallback: string): string {
    return typeof error.error?.error === 'string' ? error.error.error : fallback;
  }

  protected ticketListRoute(): string {
    return this.user()?.role === 'USER' ? '/my-tickets' : this.user()?.role === 'SUPPORT' ? '/assigned' : '/tickets';
  }

  protected cancel(): void { void this.router.navigateByUrl(this.ticketListRoute()); }
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
