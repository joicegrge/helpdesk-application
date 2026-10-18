import { Injectable, signal } from '@angular/core';
import { TicketApiItem } from '../auth.service';

export type TicketPriority = 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
export type TicketStatus = 'OPEN' | 'ASSIGNED' | 'IN_PROGRESS' | 'RESOLVED' | 'PENDING_APPROVAL' | 'PENDING_RESOLUTION_APPROVAL' | 'PENDING_USER_CONFIRMATION' | 'CLOSED';

export interface HelpdeskTicket {
  id: number;
  number: string;
  title: string;
  description: string;
  category: string;
  priority: TicketPriority;
  status: TicketStatus;
  resolutionDescription: string | null;
  assignedTo: string;
  assignedToId: number | null;
  createdBy: string;
  createdById: number;
  createdAt: string;
  initials: string;
  avatar: string;
}

export function toHelpdeskTicket(ticket: TicketApiItem): HelpdeskTicket {
  const initials = ticket.created_by_name.trim().split(/\s+/).map((part) => part[0] ?? '').join('').slice(0, 2).toUpperCase() || 'HD';
  const createdAt = new Intl.DateTimeFormat('en-US', { month: 'short', day: '2-digit', year: 'numeric' }).format(new Date(ticket.created_at));
  return {
    id: ticket.id,
    number: ticket.ticket_number,
    title: ticket.title,
    description: ticket.description,
    category: ticket.category_name,
    priority: ticket.priority,
    status: ticket.status,
    resolutionDescription: ticket.resolution_description,
    assignedTo: ticket.assigned_to_name || 'Unassigned',
    assignedToId: ticket.assigned_to,
    createdBy: ticket.created_by_name,
    createdById: ticket.created_by,
    createdAt,
    initials,
    avatar: ['blue', 'purple', 'green', 'orange'][ticket.created_by % 4],
  };
}

@Injectable({ providedIn: 'root' })
export class MockTicketService {
  readonly tickets = signal<HelpdeskTicket[]>([]);

  replaceTickets(tickets: HelpdeskTicket[]): void {
    this.tickets.set(tickets);
  }

  updateTicket(ticket: HelpdeskTicket): void {
    this.tickets.update((tickets) =>
      tickets.map((current) => current.number === ticket.number ? ticket : current),
    );
  }
}
