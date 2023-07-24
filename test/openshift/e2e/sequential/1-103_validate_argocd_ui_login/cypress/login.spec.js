describe('ArgoCD Login', () => {
    it('should log in to ArgoCD', () => {
      cy.visit('http://argocd-server:8080')
  
      // Find the username and password input fields and enter the credentials
      cy.get('#username').type('admin')
      cy.get('#password').type('admin')
  
      // Click the login button
      cy.get('#login').click()
  
      // Wait for a few seconds to allow the UI to load
      cy.wait(5000)
  
      // Check if login is successful by looking for a specific element on the dashboard page
      cy.get('#dashboard').should('be.visible')
    })
  })
