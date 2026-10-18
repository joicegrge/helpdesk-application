import { Component, inject } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';

@Component({
  selector: 'app-placeholder',
  imports: [RouterLink],
  templateUrl: './placeholder.html',
  styleUrl: './placeholder.css',
})
export class Placeholder {
  private readonly route = inject(ActivatedRoute);
  protected readonly title = String(this.route.snapshot.data['title'] ?? 'HelpDesk');
  protected readonly description = String(this.route.snapshot.data['description'] ?? 'This section will be available in a future update.');
}
