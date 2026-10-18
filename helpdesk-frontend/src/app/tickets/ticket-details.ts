import { CommonModule } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { AuthService, HelpdeskUser, TicketComment } from '../auth.service';
import { HelpdeskTicket, MockTicketService, TicketPriority, TicketStatus, toHelpdeskTicket } from './mock-ticket.service';

@Component({
  selector: 'app-ticket-details',
  imports: [CommonModule, RouterLink],
  templateUrl: './ticket-details.html',
  styleUrl: './ticket-details.css',
})
export class TicketDetails implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly auth = inject(AuthService);
  private readonly ticketData = inject(MockTicketService);
  protected readonly ticket = signal<HelpdeskTicket | null>(null);
  protected readonly user = signal<HelpdeskUser | null>(null);
  protected readonly supportUsers = signal<HelpdeskUser[]>([]);
  protected readonly loading = signal(true);
  protected readonly savingStatus = signal(false);
  protected readonly savingAssignment = signal(false);
  protected readonly savingChanges = signal(false);
  protected readonly savingPriority = signal(false);
  protected readonly selectedStatus = signal<TicketStatus>('OPEN');
  protected readonly selectedAssignee = signal('');
  protected readonly selectedPriority = signal<TicketPriority>('MEDIUM');
  protected readonly resolutionDescription = signal('');
  protected readonly actionError = signal('');
  protected readonly actionMessage = signal('');
  protected readonly comments = signal<TicketComment[]>([]);
  protected readonly commentText = signal('');
  protected readonly commentsLoading = signal(false);
  protected readonly postingComment = signal(false);
  protected readonly closureReason = signal('');
  protected readonly reviewingClosure = signal(false);
  protected readonly reviewingResolution = signal(false);
  protected readonly confirmingResolution = signal(false);
  protected readonly commentError = signal('');
  protected readonly darkMode = signal(this.readDarkMode());
  protected readonly canAssign = computed(() => this.user()?.role === 'ADMIN');
  protected readonly canConfirmResolution = computed(() => this.user()?.role === 'USER' && this.ticket()?.status === 'PENDING_USER_CONFIRMATION');
  protected readonly canUpdateStatus = computed(() => {
    const user = this.user();
    const ticket = this.ticket();
    return !!user && !!ticket && (user.role === 'ADMIN' || (user.role === 'SUPPORT' && ticket.assignedToId === user.id));
  });
  protected readonly backUrl = computed(() => this.user()?.role === 'USER' ? '/my-tickets' : this.user()?.role === 'SUPPORT' ? '/assigned' : '/tickets');

  protected statusLabel(status: TicketStatus): string {
    if (status === 'PENDING_APPROVAL') return 'Pending Admin Approval';
    if (status === 'PENDING_RESOLUTION_APPROVAL') return 'Pending Admin Resolution Review';
    if (status === 'PENDING_USER_CONFIRMATION') return 'Pending User Approval';
    return status.replace('_', ' ');
  }

  ngOnInit(): void {
    this.applyTheme();
    this.auth.currentUser().subscribe({
      next: ({ user }) => {
        this.user.set(user);
        if (user.role === 'ADMIN') this.auth.supportUsers().subscribe({ next: ({ users }) => this.supportUsers.set(users) });
      },
    });
    const number = this.route.snapshot.paramMap.get('ticketNumber');
    if (!number) { this.loading.set(false); return; }
    this.auth.ticket(number).subscribe({
      next: (response) => {
        const ticket = toHelpdeskTicket(response);
        this.ticket.set(ticket);
        this.ticketData.updateTicket(ticket);
        this.selectedStatus.set(ticket.status);
        this.selectedAssignee.set(ticket.assignedToId?.toString() ?? '');
        this.selectedPriority.set(ticket.priority);
        this.resolutionDescription.set(ticket.resolutionDescription ?? '');
        this.loading.set(false);
        this.loadComments(ticket.number);
      },
      error: () => { this.ticket.set(null); this.loading.set(false); },
    });
  }

  protected toggleTheme(): void {
    this.darkMode.update((enabled) => !enabled);
    try {
      window.localStorage.setItem('helpdesk_dark_mode', String(this.darkMode()));
    } catch {
      // The current page can still use the selected theme if storage is unavailable.
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

  protected saveStatus(): void {
    const ticket = this.ticket();
    if (!ticket || !this.canUpdateStatus() || this.savingStatus()) return;
    if (ticket.status === 'CLOSED') {
      this.actionError.set('Closed tickets cannot be edited.');
      return;
    }
    const needsResolution = this.user()?.role === 'ADMIN' && this.selectedStatus() === 'RESOLVED';
    if (needsResolution && !this.resolutionDescription().trim()) {
      this.actionError.set(`Enter a resolution description before marking this ticket ${this.selectedStatus().toLowerCase()}.`);
      this.actionMessage.set('');
      return;
    }
    if (this.user()?.role === 'SUPPORT' && (this.selectedStatus() === 'RESOLVED' || this.selectedStatus() === 'CLOSED') && !this.comments().some((comment) => comment.user_id === this.user()?.id)) {
      this.actionError.set('Post a comment explaining the work completed before resolving or requesting closure.');
      this.actionMessage.set('');
      return;
    }
    this.actionError.set(''); this.actionMessage.set(''); this.savingStatus.set(true);
    this.auth.updateTicketStatus(ticket.number, this.selectedStatus(), this.resolutionDescription().trim()).subscribe({
      next: ({ status }) => {
        this.auth.ticket(ticket.number).subscribe({
          next: (saved) => this.applySavedTicket(saved, status === 'PENDING_APPROVAL' ? 'Closure request sent to Admin for approval.' : 'Ticket status updated.'),
          error: () => this.refreshSavedTicket({ ...ticket, status: status as TicketStatus, resolutionDescription: this.resolutionDescription().trim() || ticket.resolutionDescription }, status === 'PENDING_APPROVAL' ? 'Closure request sent to Admin for approval.' : 'Ticket status updated.'),
        });
        this.savingStatus.set(false);
      },
      error: (error) => { this.actionError.set(error.error?.error ?? 'Could not update ticket status.'); this.savingStatus.set(false); },
    });
  }

  protected saveAssignment(): void {
    const ticket = this.ticket();
    if (!ticket || !this.canAssign() || this.savingAssignment()) return;
    if (ticket.status === 'CLOSED') {
      this.actionError.set('Closed tickets cannot be edited.');
      return;
    }
    const assignedTo = this.selectedAssignee() ? Number(this.selectedAssignee()) : null;
    this.actionError.set(''); this.actionMessage.set(''); this.savingAssignment.set(true);
    this.auth.assignTicket(ticket.number, assignedTo).subscribe({
      next: () => {
        this.auth.ticket(ticket.number).subscribe({
          next: (saved) => this.applySavedTicket(saved, 'Ticket assignment updated.'),
          error: () => {
            const supportUser = this.supportUsers().find((support) => support.id === assignedTo);
            this.refreshSavedTicket({ ...ticket, assignedToId: assignedTo, assignedTo: supportUser?.full_name ?? 'Unassigned' }, 'Ticket assignment updated.');
          },
        });
        this.savingAssignment.set(false);
      },
      error: (error) => { this.actionError.set(error.error?.error ?? 'Could not update ticket assignment.'); this.savingAssignment.set(false); },
    });
  }

  protected saveAdminChanges(): void {
    const ticket = this.ticket();
    if (!ticket || !this.canAssign() || this.savingChanges()) return;
    if (ticket.status === 'CLOSED') {
      this.actionError.set('Closed tickets cannot be edited.');
      return;
    }
    if (this.selectedStatus() === 'RESOLVED' && !this.resolutionDescription().trim()) {
      this.actionError.set('Enter a resolution description before marking this ticket resolved.');
      this.actionMessage.set('');
      return;
    }
    const assignedTo = this.selectedAssignee() ? Number(this.selectedAssignee()) : null;
    if (this.selectedStatus() === ticket.status && assignedTo === ticket.assignedToId) {
      this.actionError.set('There are no changes to save.');
      this.actionMessage.set('');
      return;
    }

    this.actionError.set('');
    this.actionMessage.set('');
    this.savingChanges.set(true);
    this.auth.updateTicket(ticket.number, this.selectedStatus(), assignedTo, this.resolutionDescription().trim()).subscribe({
      next: ({ status, assigned_to }) => {
        const supportUser = this.supportUsers().find((support) => support.id === assigned_to);
        this.refreshSavedTicket({ ...ticket, status: status as TicketStatus, assignedToId: assigned_to, assignedTo: supportUser?.full_name ?? 'Unassigned' }, 'Ticket changes saved.');
        this.auth.ticket(ticket.number).subscribe({
          next: (saved) => this.applySavedTicket(saved, 'Ticket changes saved.'),
        });
        this.savingChanges.set(false);
      },
      error: (error) => {
        this.actionError.set(this.errorMessage(error, 'Could not save ticket changes.'));
        this.savingChanges.set(false);
      },
    });
  }

  protected savePriority(): void {
    const ticket = this.ticket();
    if (!ticket || !this.canAssign() || this.savingPriority()) return;
    if (ticket.status === 'CLOSED') {
      this.actionError.set('Closed tickets cannot be edited.');
      return;
    }
    if (this.selectedPriority() === 'CRITICAL' && ticket.assignedToId === null) {
      this.actionError.set('Assign this ticket to a support user and save the assignment before setting its priority to CRITICAL.');
      this.actionMessage.set('');
      return;
    }
    if (this.selectedPriority() === ticket.priority) {
      this.actionError.set('Choose a different priority before saving.');
      this.actionMessage.set('');
      return;
    }
    this.actionError.set('');
    this.actionMessage.set('');
    this.savingPriority.set(true);
    this.auth.updateTicketPriority(ticket.number, this.selectedPriority()).subscribe({
      next: ({ priority }) => {
        const updated = { ...ticket, priority: priority as TicketPriority };
        this.refreshSavedTicket(updated, 'Ticket priority updated.');
        this.selectedPriority.set(updated.priority);
        this.auth.ticket(ticket.number).subscribe({
          next: (saved) => this.applySavedTicket(saved, 'Ticket priority updated.'),
        });
        this.savingPriority.set(false);
      },
      error: (error) => {
        this.actionError.set(this.errorMessage(error, 'Could not update ticket priority.'));
        this.actionMessage.set('');
        this.savingPriority.set(false);
      },
    });
  }

  protected addComment(): void {
    const ticket = this.ticket();
    const text = this.commentText().trim();
    if (!ticket || !text || this.postingComment()) {
      if (!text) this.commentError.set('Enter a comment before posting.');
      return;
    }
    if (ticket.status === 'CLOSED') {
      this.commentError.set('Closed tickets cannot be edited.');
      return;
    }
    this.commentError.set('');
    this.postingComment.set(true);
    this.auth.addTicketComment(ticket.number, text).subscribe({
      next: (comment) => {
        this.comments.update((comments) => [...comments, comment]);
        this.commentText.set('');
        this.postingComment.set(false);
      },
      error: (error) => {
        this.commentError.set(this.errorMessage(error, 'Could not post comment.'));
        this.postingComment.set(false);
      },
    });
  }

  protected reviewClosure(action: 'approve' | 'reject'): void {
    const ticket = this.ticket();
    if (!ticket || !this.canAssign() || ticket.status !== 'PENDING_APPROVAL' || this.reviewingClosure()) return;
    const reason = this.closureReason().trim();
    if (action === 'reject' && !reason) {
      this.actionError.set('Enter a reason before rejecting closure.');
      this.actionMessage.set('');
      return;
    }
    this.actionError.set('');
    this.actionMessage.set('');
    this.reviewingClosure.set(true);
    this.auth.reviewTicketClosure(ticket.number, action, reason).subscribe({
      next: ({ status }) => this.auth.ticket(ticket.number).subscribe({
        next: (saved) => {
          this.applySavedTicket(saved, action === 'approve' ? 'Admin approved closure. Waiting for the ticket creator to confirm.' : 'Closure rejected. The ticket was returned to In Progress.');
          this.closureReason.set('');
          this.loadComments(ticket.number);
          this.reviewingClosure.set(false);
        },
        error: () => {
          this.refreshSavedTicket({ ...ticket, status: status as TicketStatus }, action === 'approve' ? 'Admin approved closure. Waiting for the ticket creator to confirm.' : 'Closure rejected.');
          this.loadComments(ticket.number);
          this.reviewingClosure.set(false);
        },
      }),
      error: (error) => {
        this.actionError.set(this.errorMessage(error, 'Could not save the closure decision.'));
        this.reviewingClosure.set(false);
      },
    });
  }

  protected reviewResolution(action: 'approve' | 'reject'): void {
    const ticket = this.ticket();
    if (!ticket || !this.canAssign() || ticket.status !== 'PENDING_RESOLUTION_APPROVAL' || this.reviewingResolution()) return;
    const reason = this.closureReason().trim();
    if (action === 'reject' && !reason) {
      this.actionError.set('Enter a reason before rejecting the resolution.');
      this.actionMessage.set('');
      return;
    }
    this.actionError.set('');
    this.actionMessage.set('');
    this.reviewingResolution.set(true);
    this.auth.reviewTicketResolution(ticket.number, action, reason).subscribe({
      next: ({ status }) => this.auth.ticket(ticket.number).subscribe({
        next: (saved) => {
          this.applySavedTicket(saved, action === 'approve' ? 'Resolution approved. Waiting for the user to confirm.' : 'Resolution rejected. The ticket was returned to In Progress.');
          this.closureReason.set('');
          this.loadComments(ticket.number);
          this.reviewingResolution.set(false);
        },
        error: () => {
          this.refreshSavedTicket({ ...ticket, status: status as TicketStatus }, action === 'approve' ? 'Resolution approved. Waiting for the user to confirm.' : 'Resolution rejected.');
          this.loadComments(ticket.number);
          this.reviewingResolution.set(false);
        },
      }),
      error: (error) => {
        this.actionError.set(this.errorMessage(error, 'Could not save the resolution decision.'));
        this.reviewingResolution.set(false);
      },
    });
  }

  protected confirmResolution(): void {
    const ticket = this.ticket();
    if (!ticket || !this.canConfirmResolution() || this.confirmingResolution()) return;
    this.actionError.set('');
    this.actionMessage.set('');
    this.confirmingResolution.set(true);
    this.auth.confirmTicketResolution(ticket.number).subscribe({
      next: ({ status }) => this.auth.ticket(ticket.number).subscribe({
        next: (saved) => {
          this.applySavedTicket(saved, 'Your confirmation was saved. This ticket is now closed.');
          this.confirmingResolution.set(false);
        },
        error: () => {
          this.refreshSavedTicket({ ...ticket, status: status as TicketStatus }, 'Your confirmation was saved. This ticket is now closed.');
          this.confirmingResolution.set(false);
        },
      }),
      error: (error) => {
        this.actionError.set(this.errorMessage(error, 'Could not confirm this resolution.'));
        this.confirmingResolution.set(false);
      },
    });
  }

  private loadComments(ticketNumber: string): void {
    this.commentsLoading.set(true);
    this.auth.ticketComments(ticketNumber).subscribe({
      next: ({ comments }) => {
        this.comments.set(comments);
        this.commentsLoading.set(false);
      },
      error: (error) => {
        this.commentError.set(this.errorMessage(error, 'Could not load ticket comments.'));
        this.commentsLoading.set(false);
      },
    });
  }

  private refreshSavedTicket(ticket: HelpdeskTicket, message: string): void {
    this.ticket.set(ticket);
    this.ticketData.updateTicket(ticket);
    this.actionMessage.set(message);
    this.actionError.set('');
  }

  private applySavedTicket(response: Parameters<typeof toHelpdeskTicket>[0], message = ''): void {
    const ticket = toHelpdeskTicket(response);
    this.ticket.set(ticket);
    this.ticketData.updateTicket(ticket);
    this.selectedStatus.set(ticket.status);
    this.selectedAssignee.set(ticket.assignedToId?.toString() ?? '');
    this.selectedPriority.set(ticket.priority);
    this.resolutionDescription.set(ticket.resolutionDescription ?? '');
    if (message) this.actionMessage.set(message);
  }

  private errorMessage(error: any, fallback: string): string {
    if (typeof error?.error?.error === 'string') return error.error.error;
    if (typeof error?.error === 'string' && error.error.trim()) return error.error.trim();
    if (error?.status === 0) return 'Could not reach the HelpDesk server. Check that the Go backend is running.';
    return `${fallback} (HTTP ${error?.status ?? 'unknown'}).`;
  }
}
